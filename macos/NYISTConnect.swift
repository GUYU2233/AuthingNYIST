import AppKit
import SwiftUI
import Darwin

// The CLI is the sole config writer and networking implementation. Neither argv nor logs carry secrets.
private enum CoreFailure: Error { case failed }

final class ConnectionModel: ObservableObject {
    @Published var network = "nyist-tec"
    @Published var username = ""
    @Published var password = ""
    @Published var background = true
    @Published var startMinimized = false
    @Published var busy = true
    @Published var loaded = false
    @Published var keepingAlive = false
    @Published var message = "正在读取配置…"
    private let worker = DispatchQueue(label: "AuthingNYIST.core")
    private var document: [String: Any] = [:] // Preserve settings not exposed by this GUI.
    private var daemon: Process? // Access only on worker.
    private var quitting = false
    let configURL: URL
    private let executable: URL

    init() {
        configURL = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("AuthingNYIST/config.json")
        executable = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/nyist-core")
    }

    // Runs only on the worker. stdout (including --config-json's password) stays in memory.
    private func invoke(_ flag: String, input: Data? = nil) throws -> Data {
        let process = Process()
        process.executableURL = executable
        process.arguments = ["--config", configURL.path, flag]
        let output = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        let pipe = Pipe()
        if input == nil { process.standardInput = FileHandle.nullDevice }
        else { process.standardInput = pipe }
        try process.run()
        defer {
            try? pipe.fileHandleForWriting.close()
            // Also reap on stdin write/read errors so a failed operation cannot leave a child behind.
            if process.isRunning { kill(process.processIdentifier, SIGKILL) }
            process.waitUntilExit()
        }
        // Never block the UI; bound a stuck CLI, and escalate if SIGTERM is ignored.
        let timeout = DispatchWorkItem { if process.isRunning { process.terminate() } }
        let killTimeout = DispatchWorkItem { if process.isRunning { kill(process.processIdentifier, SIGKILL) } }
        DispatchQueue.global().asyncAfter(deadline: .now() + 45, execute: timeout)
        DispatchQueue.global().asyncAfter(deadline: .now() + 48, execute: killTimeout)
        defer { timeout.cancel(); killTimeout.cancel() }
        if let data = input {
            // Config is small; the core reads all stdin before returning a result.
            try pipe.fileHandleForWriting.write(contentsOf: data)
            try pipe.fileHandleForWriting.close()
        }
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0, data.count <= 1024 * 1024 else { throw CoreFailure.failed }
        return data
    }

    func load(completion: @escaping () -> Void) {
        worker.async {
            do {
                let data = try self.invoke("--config-json")
                guard let config = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let network = config["network"] as? String,
                      ["nyist-tec", "nyist-stu"].contains(network),
                      let username = config["username"] as? String,
                      let password = config["password"] as? String else { throw CoreFailure.failed }
                DispatchQueue.main.async {
                    self.document = config
                    self.network = network; self.username = username; self.password = password
                    self.background = config["background"] as? Bool ?? true
                    self.startMinimized = config["startMinimized"] as? Bool ?? false
                    self.loaded = true; self.busy = false
                    self.message = "配置已读取；尚未查询网络状态。"
                    completion()
                }
            } catch {
                DispatchQueue.main.async {
                    self.busy = false
                    self.message = "无法读取配置。为保护已有数据，保存与连接已禁用。请检查配置后重新打开应用。"
                    completion()
                }
            }
        }
    }

    private func snapshot() throws -> Data {
        var next = document
        next["network"] = network; next["username"] = username; next["password"] = password
        next["background"] = background; next["startMinimized"] = startMinimized
        return try JSONSerialization.data(withJSONObject: next)
    }

    private func startDaemon() throws {
        guard daemon == nil else { return }
        let process = Process()
        process.executableURL = executable
        process.arguments = ["--config", configURL.path, "--daemon"]
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        process.terminationHandler = { [weak self] ended in
            guard let owner = self else { return }
            owner.worker.async {
                guard owner.daemon === ended else { return }
                owner.daemon = nil
                DispatchQueue.main.async {
                    owner.keepingAlive = false
                    if !owner.quitting { owner.message = "保活进程已停止。请查询状态；需要时重新连接。" }
                }
            }
        }
        try process.run()
        daemon = process
    }

    private func stopDaemon() {
        guard let process = daemon else { return }
        daemon = nil // Suppress expected termination notifications.
        if process.isRunning {
            process.terminate()
            let force = DispatchWorkItem { if process.isRunning { kill(process.processIdentifier, SIGKILL) } }
            DispatchQueue.global().asyncAfter(deadline: .now() + 5, execute: force)
            process.waitUntilExit()
            force.cancel()
        }
    }

    // One operation at a time. No independent GUI keepalive timer and no status/logout racing the daemon.
    func perform(_ action: String) {
        guard loaded, !busy, !quitting else { return }
        guard !keepingAlive || action == "stop" || action == "disconnect" else { return }
        let input: Data
        do { input = try snapshot() } catch { message = "无法处理配置。"; return }
        busy = true
        message = "正在处理…"
        worker.async {
            var result = "操作已完成。"
            var alive = false
            do {
                switch action {
                case "save":
                    _ = try self.invoke("--save-config-stdin", input: input)
                    result = "配置已保存（本地明文，权限 0600）。"
                case "connect":
                    _ = try self.invoke("--save-config-stdin", input: input)
                    _ = try self.invoke("--connect")
                    try self.startDaemon()
                    alive = true
                    result = "连接成功；核心保活运行中。"
                case "disconnect":
                    self.stopDaemon()
                    _ = try self.invoke("--disconnect")
                    result = "已请求注销校园网；保活已停止。"
                case "stop":
                    self.stopDaemon()
                    result = "保活已停止，未主动注销校园网。"
                case "status":
                    let data = try self.invoke("--status-json")
                    guard let status = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                          let online = status["Online"] as? Bool else { throw CoreFailure.failed }
                    // Do not surface untrusted server messages or credentials.
                    result = online ? "校园网在线。" : "校园网未在线。"
                default: throw CoreFailure.failed
                }
            } catch {
                result = "操作失败。请检查配置、校园网络和核心程序后重试。"
                alive = self.daemon != nil
            }
            DispatchQueue.main.async {
                self.keepingAlive = alive; self.busy = false; self.message = result
            }
        }
    }

    func shutdown(completion: @escaping () -> Void) {
        quitting = true; busy = true
        worker.async {
            self.stopDaemon() // Stopping a foreground daemon does not log out.
            DispatchQueue.main.async { completion() }
        }
    }
}

struct ContentView: View {
    @ObservedObject var model: ConnectionModel
    @State private var confirmDisconnect = false
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("NYIST Connect").font(.largeTitle).bold()
            Text("南阳理工学院 · 校园网连接").foregroundColor(.secondary)
            Group {
                Picker("网络", selection: $model.network) {
                    Text("nyist-tec").tag("nyist-tec")
                    Text("nyist-stu").tag("nyist-stu")
                }.pickerStyle(SegmentedPickerStyle())
                TextField("用户名", text: $model.username)
                SecureField("密码", text: $model.password)
                Toggle("关闭窗口后留在菜单栏", isOn: $model.background)
                Toggle("下次打开应用时收起窗口（不是开机自启动）", isOn: $model.startMinimized)
            }.disabled(model.busy || !model.loaded || model.keepingAlive)
            HStack {
                Button("保存") { model.perform("save") }.disabled(model.keepingAlive)
                Button("连接并保活") { model.perform("connect") }.disabled(model.keepingAlive)
                Button("查询状态") { model.perform("status") }.disabled(model.keepingAlive)
                Button("断开…") { confirmDisconnect = true }
            }.disabled(model.busy || !model.loaded)
            Button("停止保活（不注销）") { model.perform("stop") }
                .disabled(model.busy || !model.keepingAlive)
            Text(model.message).fixedSize(horizontal: false, vertical: true).frame(minHeight: 40, alignment: .topLeading)
            Divider()
            Text("密码以明文保存在 Application Support/AuthingNYIST/config.json，文件权限 0600；未使用 Keychain。请保护本机账号。退出应用会停止保活，但不会注销校园网。保活运行时请先停止，再修改配置或查询状态。")
                .font(.footnote).foregroundColor(.secondary).fixedSize(horizontal: false, vertical: true)
        }
        .padding(24).frame(width: 520)
        .alert(isPresented: $confirmDisconnect) {
            Alert(title: Text("确认断开校园网？"), message: Text("将先停止保活，再向认证服务器请求注销，网络连接可能中断。"),
                  primaryButton: .destructive(Text("断开")) { model.perform("disconnect") }, secondaryButton: .cancel(Text("取消")))
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private let model = ConnectionModel()
    private var window: NSWindow!
    private var item: NSStatusItem!
    private var lockFD: Int32 = -1
    private var terminating = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        // A second GUI must not create another keepalive against the same configuration.
        let directory = model.configURL.deletingLastPathComponent()
        do { try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700]) }
        catch { NSApp.terminate(nil); return }
        lockFD = open(directory.appendingPathComponent("gui.lock").path, O_CREAT | O_RDWR | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard lockFD >= 0, flock(lockFD, LOCK_EX | LOCK_NB) == 0 else {
            let alert = NSAlert(); alert.messageText = "应用已运行或无法锁定配置目录。"
            alert.informativeText = "请通过菜单栏打开现有窗口，或检查本地配置目录权限。"
            alert.runModal(); NSApp.terminate(nil); return
        }
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 570, height: 520), styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "NYIST Connect"; window.delegate = self; window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: ContentView(model: model))
        window.center()
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let image = NSImage(systemSymbolName: "network", accessibilityDescription: "NYIST Connect") { item.button?.image = image }
        else { item.button?.title = "NYIST" }
        let menu = NSMenu()
        menu.addItem(withTitle: "打开 NYIST Connect", action: #selector(showWindow), keyEquivalent: "o").target = self
        menu.addItem(NSMenuItem.separator())
        menu.addItem(withTitle: "退出（停止保活，不注销）", action: #selector(quit), keyEquivalent: "q").target = self
        item.menu = menu
        let mainMenu = NSMenu()
        let applicationItem = NSMenuItem(); mainMenu.addItem(applicationItem)
        let applicationMenu = NSMenu(); applicationItem.submenu = applicationMenu
        applicationMenu.addItem(withTitle: "退出 NYIST Connect", action: #selector(quit), keyEquivalent: "q").target = self
        let editItem = NSMenuItem(); mainMenu.addItem(editItem)
        let editMenu = NSMenu(title: "编辑"); editItem.submenu = editMenu
        editMenu.addItem(withTitle: "撤销", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        NSApp.mainMenu = mainMenu
        model.load {
            if self.model.loaded && self.model.startMinimized {
                NSApp.setActivationPolicy(.accessory)
            } else { self.showWindow() }
        }
    }
    @objc private func showWindow() {
        NSApp.setActivationPolicy(.regular)
        window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    }
    @objc private func quit() { NSApp.terminate(nil) }
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        if model.background { sender.orderOut(nil); NSApp.setActivationPolicy(.accessory) }
        else { NSApp.terminate(nil) }
        return false
    }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool { showWindow(); return true }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard !terminating else { return .terminateLater }
        terminating = true
        model.shutdown {
            if self.lockFD >= 0 { close(self.lockFD); self.lockFD = -1 }
            NSApp.reply(toApplicationShouldTerminate: true)
        }
        return .terminateLater
    }
}

@main
struct NYISTMain {
    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.regular)
        withExtendedLifetime(delegate) { app.run() }
    }
}
