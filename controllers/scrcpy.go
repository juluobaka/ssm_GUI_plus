// Copyright (C) 2024, 2025 kvarenzn
// SPDX-License-Identifier: GPL-3.0-or-later

package controllers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kvarenzn/ssm/adb"
	"github.com/kvarenzn/ssm/common"
	"github.com/kvarenzn/ssm/config"
	"github.com/kvarenzn/ssm/decoders/av"
	"github.com/kvarenzn/ssm/log"
	"github.com/kvarenzn/ssm/stage"
)

type ScrcpyController struct {
	device    *adb.Device
	sessionID string

	listener      net.Listener
	videoSocket   net.Conn
	controlSocket net.Conn

	width        int
	height       int
	deviceWidth  int
	deviceHeight int
	codecID      string
	decoder      *av.AVDecoder
	cRunning     bool
	vRunning     bool

	closing atomic.Bool

	fatalOnce sync.Once
	fatalCh   chan error

	frameMu     sync.RWMutex
	latestFrame *ScrcpyFrame
	frameFn     func(ScrcpyFrame)
}

// ScrcpyFrame is a compact grayscale-friendly frame snapshot for analyzers.
// Plane0 typically represents Y/luma for the common YUV formats from scrcpy.
type ScrcpyFrame struct {
	PTS         int64
	Width       int
	Height      int
	PixelFormat int
	Plane0      []byte
	CapturedAt  time.Time
}

func NewScrcpyController(device *adb.Device) *ScrcpyController {
	return &ScrcpyController{
		device:    device,
		sessionID: fmt.Sprintf("%08x", rand.Int31()),
		fatalCh:   make(chan error, 1),
	}
}

func (c *ScrcpyController) Err() <-chan error {
	return c.fatalCh
}

func (c *ScrcpyController) reportFatal(err error) {
	if c.closing.Load() {
		return
	}
	if err == nil {
		err = errors.New("scrcpy disconnected")
	}
	c.fatalOnce.Do(func() {
		select {
		case c.fatalCh <- err:
		default:
		}
		close(c.fatalCh)
		c.frameMu.Lock()
		c.latestFrame = nil
		c.frameMu.Unlock()
		_ = c.Close()
	})
}

func tryListen(host string, port int) (net.Listener, int) {
	for {
		addr := fmt.Sprintf("%s:%d", host, port)
		listen, err := net.Listen("tcp", addr)
		if err == nil {
			return listen, port
		}

		port++
	}
}

func readFull(conn net.Conn, buf []byte) error {
	_, err := io.ReadFull(conn, buf)
	return err
}

const testFromPort = 27188

func (c *ScrcpyController) Open(filepath string, version string) error {
	localPort := testFromPort + rand.Intn(100)
	localName := fmt.Sprintf("localabstract:scrcpy_%s", c.sessionID)

	// Create ADB Forward: PC_PORT -> Phone_Socket
	err := c.device.Forward(fmt.Sprintf("tcp:%d", localPort), localName, false, false)
	if err != nil {
		return fmt.Errorf("failed to create adb forward: %v", err)
	}
	log.Infof("[ADB] Forward tunnel created: tcp:%d -> %s", localPort, localName)

	f, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := c.device.Push(f, "/data/local/tmp/scrcpy-server.jar"); err != nil {
		return err
	}

	log.Debugln("`scrcpy-server` pushed to gaming device.")

	// Start server in background
	go func() {
		log.Infof("[ADB] Starting scrcpy-server on device (version %s)...", version)
		// Using tunnel_forward=true so server LISTENS on localName
		// Using scid to ensure unique socket name
		cmdLine := fmt.Sprintf("CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server %s log_level=debug audio=false clipboard_autosync=false max_size=720 video_bit_rate=2000000 tunnel_forward=true scid=%s send_device_meta=false", version, c.sessionID)
		cmd := fmt.Sprintf("sh -c %q", cmdLine+" 2>&1")
		result, err := c.device.Sh(cmd)
		if err != nil {
			log.Warnf("[ADB] scrcpy-server process ended: %v", err)
		}
		res := strings.TrimSpace(result)
		if res != "" {
			if len(res) > 5000 {
				res = res[len(res)-5000:]
			}
			log.Warnf("[ADB] scrcpy-server output (tail):\n%s", res)
		}
	}()

	// Wait for server to start listening
	log.Infof("[ADB] Waiting 5s for scrcpy server to start...")
	time.Sleep(5 * time.Second)

	// Connect to the forwarded port (Video Socket)
	videoSocket, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to video socket: %v", err)
	}
	c.videoSocket = videoSocket
	log.Infoln("[ADB] Video socket connected.")

	// Connect to the forwarded port (Control Socket)
	controlSocket, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to control socket: %v", err)
	}
	c.controlSocket = controlSocket
	log.Infoln("[ADB] Control socket connected.")

	// Wait a bit for the server to initialize and send the header
	log.Infof("[ADB] Waiting 500ms for scrcpy server to initialize...")
	time.Sleep(500 * time.Millisecond)

	c.codecID = "h264" // Default

	// Set a read deadline for the header
	log.Infof("[ADB] Setting read deadline and waiting for stream header...")
	videoSocket.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Read exactly 13 bytes for the header: [dummy:1][codec:4][width:4][height:4]
	dimBuf := make([]byte, 13)
	if err := readFull(videoSocket, dimBuf); err != nil {
		log.Warnf("[ADB] Failed to read stream header: %v", err)
		return fmt.Errorf("failed to read stream header: %v", err)
	}
	log.Infof("[ADB] Stream header received (hex): %x", dimBuf)

	// Clear the deadline after header is received
	videoSocket.SetReadDeadline(time.Time{})

	// Parse based on official scrcpy 3.x logic
	// Skip the dummy byte if it's 0x00
	offset := 0
	if dimBuf[0] == 0 {
		offset = 1
	}

	c.codecID = string(dimBuf[offset : offset+4])
	log.Infof("[ADB] Codec detected: %s", c.codecID)

	// Read Width and Height
	c.width = int(binary.BigEndian.Uint32(dimBuf[offset+4 : offset+8]))
	c.height = int(binary.BigEndian.Uint32(dimBuf[offset+8 : offset+12]))
	log.Infof("[ADB] Stream resolution: %dx%d", c.width, c.height)

	// Always enable video decode for GUI vision/preview
	c.decoder, err = av.NewAVDecoder(c.codecID)
	if err != nil {
		return fmt.Errorf("failed to create decoder for %s: %v", c.codecID, err)
	}
	firstDecodedLogged := false
	c.decoder.SetFrameHandler(func(f av.DecodedFrame) {
		frame := ScrcpyFrame{
			PTS:         f.PTS,
			Width:       f.Width,
			Height:      f.Height,
			PixelFormat: f.PixelFormat,
			Plane0:      append([]byte(nil), f.Plane0...),
			CapturedAt:  time.Now(),
		}

		c.frameMu.Lock()
		c.latestFrame = &frame
		fn := c.frameFn
		c.frameMu.Unlock()

		if !firstDecodedLogged && len(frame.Plane0) > 0 {
			log.Infof("[SCRCPY] First decoded frame: %dx%d plane0=%d", frame.Width, frame.Height, len(frame.Plane0))
			firstDecodedLogged = true
		}

		if fn != nil {
			fn(frame)
		}
	})

	return c.startStreaming(videoSocket, controlSocket)
}

func (c *ScrcpyController) startStreaming(videoSocket, controlSocket net.Conn) error {
	c.cRunning = true
	c.vRunning = true

	go func() {
		msgTypeBuf := make([]byte, 1)
		sizeBuf := make([]byte, 4)
		for c.cRunning {
			if err := readFull(controlSocket, msgTypeBuf); err != nil {
				if c.closing.Load() || !c.cRunning {
					break
				}
				c.reportFatal(err)
				break
			}

			if err := readFull(controlSocket, sizeBuf); err != nil {
				if c.closing.Load() || !c.cRunning {
					break
				}
				c.reportFatal(err)
				break
			}

			size := binary.BigEndian.Uint32(sizeBuf)
			bodyBuf := make([]byte, size)
			if err := readFull(controlSocket, bodyBuf); err != nil {
				if c.closing.Load() || !c.cRunning {
					break
				}
				c.reportFatal(err)
				break
			}
		}

		c.cRunning = false
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Warnf("[SCRCPY] Video goroutine panic: %v", r)
			}
		}()

		ptsBuf := make([]byte, 8)
		sizeBuf := make([]byte, 4)
		firstPacketLogged := false

		for c.vRunning {
			if err := readFull(videoSocket, ptsBuf); err != nil {
				log.Warnf("[SCRCPY] readFull pts failed: %v", err)
				if c.closing.Load() || !c.vRunning {
					break
				}
				c.reportFatal(err)
				break
			}
			pts := binary.BigEndian.Uint64(ptsBuf)

			if err := readFull(videoSocket, sizeBuf); err != nil {
				log.Warnf("[SCRCPY] readFull size failed: %v", err)
				if c.closing.Load() || !c.vRunning {
					break
				}
				c.reportFatal(err)
				break
			}
			size := binary.BigEndian.Uint32(sizeBuf)

			if c.decoder == nil {
				log.Warnf("[SCRCPY] decoder is nil, discarding %d bytes", size)
				io.CopyN(io.Discard, videoSocket, int64(size))
				continue
			}

			data := make([]byte, size)
			if err := readFull(videoSocket, data); err != nil {
				log.Warnf("[SCRCPY] readFull data failed: %v", err)
				if c.closing.Load() || !c.vRunning {
					break
				}
				c.reportFatal(err)
				break
			}

			if !firstPacketLogged && len(data) > 0 {
				log.Infof("[SCRCPY] First video packet: pts=%d size=%d data[0:20]=%x", pts, size, data[:min(20, len(data))])
				firstPacketLogged = true
			}

			if err := c.decoder.Decode(pts, data); err != nil {
				log.Warnf("[SCRCPY] Decode failed: %v", err)
			}
		}
		log.Infof("[SCRCPY] Video goroutine ended (vRunning=%v)", c.vRunning)
		c.vRunning = false
	}()

	return nil
}

func (c *ScrcpyController) Encode(action common.TouchAction, x, y int32, pointerID uint64) []byte {
	data := make([]byte, 32)
	data[0] = 2 // type: SC_CONTROL_MSG_TYPE_INJECT_TOUCH_EVENT
	data[1] = byte(action)
	binary.BigEndian.PutUint64(data[2:], pointerID)
	binary.BigEndian.PutUint32(data[10:], uint32(x))
	binary.BigEndian.PutUint32(data[14:], uint32(y))
	binary.BigEndian.PutUint16(data[18:], uint16(c.width))
	binary.BigEndian.PutUint16(data[20:], uint16(c.height))
	binary.BigEndian.PutUint16(data[22:], 0xffff)
	binary.BigEndian.PutUint32(data[24:], 1) // AMOTION_EVENT_BUTTON_PRIMARY
	binary.BigEndian.PutUint32(data[28:], 1) // AMOTION_EVENT_BUTTON_PRIMARY
	return data
}

func (c *ScrcpyController) touch(action common.TouchAction, x, y int32, pointerID uint64) {
	if c.deviceWidth > 0 && c.deviceHeight > 0 && c.width > 0 && c.height > 0 {
		sx := int64(x) * int64(c.width) / int64(c.deviceWidth)
		sy := int64(y) * int64(c.height) / int64(c.deviceHeight)
		if sx < 0 {
			sx = 0
		} else if sx >= int64(c.width) {
			sx = int64(c.width - 1)
		}
		if sy < 0 {
			sy = 0
		} else if sy >= int64(c.height) {
			sy = int64(c.height - 1)
		}
		x = int32(sx)
		y = int32(sy)
	}
	c.Send(c.Encode(action, x, y, pointerID))
}

func (c *ScrcpyController) Down(pointerID uint64, x, y int) {
	c.touch(common.TouchDown, int32(x), int32(y), pointerID)
}

func (c *ScrcpyController) Move(pointerID uint64, x, y int) {
	c.touch(common.TouchMove, int32(x), int32(y), pointerID)
}

func (c *ScrcpyController) Up(pointerID uint64, x, y int) {
	c.touch(common.TouchUp, int32(x), int32(y), pointerID)
}

func (c *ScrcpyController) Close() error {
	c.closing.Store(true)
	c.cRunning = false
	c.vRunning = false

	if c.videoSocket != nil {
		c.videoSocket.Close()
	}

	if c.controlSocket != nil {
		c.controlSocket.Close()
	}

	if c.decoder != nil {
		c.decoder.Drop()
		c.decoder = nil
	}

	if c.listener != nil {
		return c.listener.Close()
	}
	return nil
}

func (c *ScrcpyController) SetFrameHandler(fn func(ScrcpyFrame)) {
	c.frameMu.Lock()
	c.frameFn = fn
	c.frameMu.Unlock()
}

func (c *ScrcpyController) LatestFrame() (ScrcpyFrame, bool) {
	c.frameMu.RLock()
	defer c.frameMu.RUnlock()
	if c.latestFrame == nil {
		return ScrcpyFrame{}, false
	}
	f := *c.latestFrame
	f.Plane0 = append([]byte(nil), c.latestFrame.Plane0...)
	return f, true
}

func (c *ScrcpyController) Preprocess(rawEvents common.RawVirtualEvents, turnRight bool, dc *config.DeviceConfig, calc stage.JudgeLinePositionCalculator) []common.ViscousEventItem {
	width, height := float64(dc.Height), float64(dc.Width)
	x1, x2, yy := calc(width, height)
	mapper := func(x, y float64) (int, int) {
		return int(math.Round(x1 + (x2-x1)*x)), int(math.Round(yy - (yy-height/2)*y))
	}

	result := []common.ViscousEventItem{}
	currentFingers := map[int]bool{}
	for _, events := range rawEvents {
		var data []byte
		for _, event := range events.Events {
			if event.PointerID < 0 {
				log.Fatalf("invalid pointer id: %d", event.PointerID)
			}
			x, y := mapper(event.X, event.Y)
			action, ok := common.NormalizeTouchAction(event.Action)
			if !ok {
				log.Fatalf("unknown touch action: %d\n", event.Action)
			}
			switch action {
			case common.TouchDown:
				if currentFingers[event.PointerID] {
					log.Fatalf("pointer `%d` is already on screen", event.PointerID)
				}
				currentFingers[event.PointerID] = true
			case common.TouchMove:
				if !currentFingers[event.PointerID] {
					log.Fatalf("pointer `%d` is not on screen", event.PointerID)
				}
			case common.TouchUp:
				if !currentFingers[event.PointerID] {
					log.Fatalf("pointer `%d` is not on screen", event.PointerID)
				}
				delete(currentFingers, event.PointerID)
			}

			data = append(data, c.Encode(action, int32(x), int32(y), uint64(event.PointerID))...)
		}

		result = append(result, common.ViscousEventItem{
			Timestamp: events.Timestamp,
			Data:      data,
		})
	}

	return result
}

var sendCount int

func (c *ScrcpyController) Send(data []byte) {
	out := data
	var ox, oy, sx, sy int64
	hasScaleLog := false
	if c.controlSocket != nil && c.deviceWidth > 0 && c.deviceHeight > 0 && c.width > 0 && c.height > 0 && len(data)%32 == 0 {
		buf := make([]byte, len(data))
		copy(buf, data)
		for i := 0; i+32 <= len(buf); i += 32 {
			if buf[i] != 2 {
				continue
			}
			x := int64(binary.BigEndian.Uint32(buf[i+10 : i+14]))
			y := int64(binary.BigEndian.Uint32(buf[i+14 : i+18]))
			sx := x * int64(c.width) / int64(c.deviceWidth)
			sy := y * int64(c.height) / int64(c.deviceHeight)
			if sx < 0 {
				sx = 0
			} else if sx >= int64(c.width) {
				sx = int64(c.width - 1)
			}
			if sy < 0 {
				sy = 0
			} else if sy >= int64(c.height) {
				sy = int64(c.height - 1)
			}
			binary.BigEndian.PutUint32(buf[i+10:i+14], uint32(sx))
			binary.BigEndian.PutUint32(buf[i+14:i+18], uint32(sy))
			binary.BigEndian.PutUint16(buf[i+18:i+20], uint16(c.width))
			binary.BigEndian.PutUint16(buf[i+20:i+22], uint16(c.height))
			if !hasScaleLog {
				ox, oy = x, y
				hasScaleLog = true
				// record first scaled values for logging below
				// reuse sx/sy from this iteration
				// shadowed sx/sy above; recompute for log
				sx = int64(binary.BigEndian.Uint32(buf[i+10 : i+14]))
				sy = int64(binary.BigEndian.Uint32(buf[i+14 : i+18]))
			}
		}
		out = buf
	}

	n, err := c.controlSocket.Write(out)
	sendCount++
	if sendCount <= 5 {
		action := out[1]
		x := int(binary.BigEndian.Uint32(out[10:]))
		y := int(binary.BigEndian.Uint32(out[14:]))
		pid := binary.BigEndian.Uint64(out[2:])
		if hasScaleLog {
			log.Infof("[SEND] #%d action=%d ptr=%d x=%d y=%d n=%d err=%v (scale %dx%d->%dx%d %d,%d->%d,%d)", sendCount, action, pid, x, y, n, err, c.deviceWidth, c.deviceHeight, c.width, c.height, ox, oy, sx, sy)
		} else {
			log.Infof("[SEND] #%d action=%d ptr=%d x=%d y=%d n=%d err=%v", sendCount, action, pid, x, y, n, err)
		}
	}
	if err != nil {
		log.Warnf("failed to send control data through control socket: %v", err)
		c.reportFatal(err)
		return
	}

	if n != len(out) {
		log.Warnf("partial control data sent: expect %d bytes, sent %d bytes", len(out), n)
		c.reportFatal(io.ErrShortWrite)
		return
	}
}

func (c *ScrcpyController) ResetTouch() {
	if c.controlSocket == nil {
		return
	}
	for i := 0; i < 10; i++ {
		data := c.Encode(common.TouchUp, 0, 0, uint64(i))
		n, err := c.controlSocket.Write(data)
		if err != nil {
			log.Warnf("ResetTouch failed to send: %v", err)
			c.reportFatal(err)
			return
		}
		if n != len(data) {
			log.Warnf("ResetTouch partial write: %d vs %d", n, len(data))
			c.reportFatal(io.ErrShortWrite)
			return
		}
	}
}

func (c *ScrcpyController) Reset() {
}

func (c *ScrcpyController) SetDeviceSize(width, height int) {
	c.deviceWidth = width
	c.deviceHeight = height
}
