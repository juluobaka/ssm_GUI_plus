<p align="center">
    <a href="./">
        <img src="imgs/page.png" alt="ssm-gui-banner"/>
    </a>
    <br>
    <strong>A Web-based GUI for automated mobile rhythm game playback and chart parsing.</strong>
</p>


# SSM Web GUI（改进版）

本项目基于 [kvarenzn/ssm](https://github.com/kvarenzn/ssm) 的核心架构，并在 Web GUI 分支的基础上继续扩展。

支持 BanG Dream / Project Sekai（谱面解析 + 自动打歌 Web 控制台）。

## 仓库结构
- `source/`：源码入口提示（源码工程在仓库根目录）
- `release/`：发布版目录（用于打包上传到 GitHub Releases）

注意：为了避免仓库体积膨胀，`*.dll / *.exe / scrcpy-server-* / all.*.json / config.json` 默认都在 `.gitignore` 中被忽略，不会跟随源码一起提交。发布版请打包 `release/windows-full/` 上传到 GitHub Releases。

## 相对原版新增内容（重点）

### ADB 投屏 / 触控（scrcpy 3.3.1）
- 使用 ADB forward + scrcpy-server 3.3.1（兼容部分机型/Android 14 环境）
- 修复 scrcpy forward 模式 dummy byte/header 解析与视频包读取边界问题
- `/api/screen` 改为浏览器兼容的 MJPEG 输出，并提供 `/api/screen?once=1` 单帧调试

### 视觉自动开始（Vision）
- 7 点检测框支持实时可视化校准（Y / X / 点间距）
- 检测改为按钮式流程：按下开始检测 → 检测到后开始打歌 → 自动停止检测
- 支持“检测到后延迟开始”（可调 ms）

![自动开始校准与检测](/imgs/vision_autostart.png "Vision Auto-Start")

自动开始使用说明（推荐流程）：
- 进入 Play Control，先完成载入到 READY
- 调整 Detection Y / Detection X / 点间距，把 7 个检测框对齐判定线 7 轨
- 点击“开始检测”，检测到开局白点后会按你设置的“开始延迟”自动开始

### 其他
- “不全连/提前结束”选项：可在接近结尾时提前停止（可调提前时间）
- 新增 UI 文案已加入多语言（简中/繁中/日/英）

## What's New

### 🎵 Smart Song Search
![Keyword Search](/imgs/retrival.png "retrival")
- Real-time search across the full Bestdori library
- One-click difficulty selection (EASY → SPECIAL)
- Still supports manual Song ID and custom `.txt` chart paths for the power users

### ▶️ Playback Control Panel
![Control Panel](/imgs/paly_page.png "Control Panel")
- **Now Playing card** — jacket art, song title, band, difficulty, all in one glance
- **Interrupt & restart instantly** — hit Stop, then Start again without re-loading anything
- **Offset adjustment** — fine-tune timing on the fly with keyboard shortcuts

### ✨ Auto-Start (Vision)
![Vision Auto-Start](/imgs/vision_autostart.png "Vision Auto-Start")
- Calibrate 7 markers (Y / X / spacing) on the judgement line
- Click Start Detect, once the opening note is detected it will auto-start then stop detecting


## Requirements 
1. A computer, a mobile phone, and a data transfer cable
2. A pair of skillful hands

## Quick Start / 快速开始

### English

1. **Download**
    - 推荐直接使用 `release/windows-full/` 打包成 zip 上传到 GitHub Releases（或从 Releases 下载解压）。

2. **Start the program**
    - Double-click `ssm.exe`, or run:
      ```bash
      ./ssm.exe
      ```
    - The UI should open automatically at `http://127.0.0.1:8765`.
    - If it does not open, visit the address manually in your browser.

3. **Prepare your phone**
    - Connect your phone to your PC with a USB cable.
    - Enable **USB debugging / ADB debugging** on the phone.

4. **Copy game resources to PC**
    - Copy and extract the game resource pack to your computer.
    - Also copy the device data directory:
      - BanG Dream example:
         ```bash
         adb pull /sdcard/Android/data/jp.co.craftegg.band/files/data/
         ```
      - Path format:
         `/sdcard/Android/data/{game_package_name}/files/data/`

5. **Set up device in GUI**
    - Open the **Settings** page.
    - Add your device (serial number can be auto-detected or selected from dropdown).
    - Choose connection type: **HID** or **ADB**.

6. **Load song and start playback**
    - In the main flow, go through: **Song Setup -> Play Control -> Start**.
    - When the first note reaches the judgement line, press **Start** (or keyboard **Enter** / **Space**).
    - If timing is early/late, adjust **Offset/Delay** and retry.

> Legacy command-line usage is still supported. You can append original CLI parameters as before.
> See [kvarenzn's Usage Guide](https://github.com/kvarenzn/ssm/blob/main/docs/USAGE.md).


1. **下载与解压**
   * 推荐使用 `release/windows-full/` 目录打包成 zip 上传到 GitHub Releases；用户下载后解压即可用。

2. **启动程序**

   * 直接双击 `ssm.exe`，或用终端运行：

     ```bash
     ./ssm.exe
     ```
   * 程序会尝试自动打开浏览器到 `http://127.0.0.1:8765`。
   * 如果没有自动打开，请手动输入网址。

3. **连接并准备手机或模拟器**

   * 手机请用 USB 线连接电脑；如果使用模拟器，请先确认 ADB 已启用。
   * 在手机上开启 **USB 调试 / ADB 调试**。
   * 可使用以下命令确认设备是否已连接：

     ```bash
     adb devices
     ```

4. **准备游戏资源**

   * 将游戏资源包复制到电脑并解压。
   * 同时把手机中的数据目录复制到电脑：

     * BanG Dream 示例：

       ```bash
       adb pull /sdcard/Android/data/jp.co.craftegg.band/files/data/
       ```
     * 通用路径：
       `/sdcard/Android/data/{游戏包名}/files/data/`

5. **在 GUI 中设置设备**

   * 进入 **Settings** 页面添加设备。
   * 序列号可以自动检测，或从下拉菜单选择。
   * 连接方式选择 **HID** 或 **ADB**。

6. **选歌并开始**

   * 按流程操作：**Song Setup -> Play Control -> Start**。
   * 当第一个音符接近判定线时，按 **Start**（或键盘 **Enter** / **Space**）。
   * 如果时机偏早或偏晚，可以调整 **Offset/Delay** 后重试。

> 仍可使用传统命令行参数方式启动。
> 详细参数请参考 [kvarenzn 的使用指南](https://github.com/kvarenzn/ssm/blob/main/docs/USAGE.md)。



## Disclaimer
This program was heavily developed with the assistance of AI. Please use it at your own discretion and feel free to report any unexpected bugs or issues.

> [!IMPORTANT]
> **This project is developed for personal learning and research purposes only. The stability and applicability of its functions are not guaranteed.**
>
> * **Non-Affiliation**: This project is an independent third-party tool and is **not** affiliated with, authorized by, or associated with any game developers, publishers, or related organizations.
> * **Risk of Use**: Use of this project may violate the service terms of the games or platforms involved, potentially leading to account suspension, bans, or data corruption.
> * **Limitation of Liability**: The author assumes no responsibility for any consequences resulting from the use of this project. Users are advised to evaluate the risks and use the software with caution.

## Future Projects
1. Mobile Porting & Deployment: Porting the application to mobile devices for use on non-rooted hardware (leveraging ADB tools such as Shizuku).

2. Automated Rhythm Game Playback: Implementation of image recognition for automated gameplay in rhythm games.

---

## 📜 License & Credits

* **Core Play Logic & Chart Parsing**: Credited to the original author [kvarenzn](https://github.com/kvarenzn/ssm).
* **Web GUI Implementation**: Custom integrated control panel developed specifically for this branch.
* This project is licensed under the **GPL-3.0-or-later** license.
