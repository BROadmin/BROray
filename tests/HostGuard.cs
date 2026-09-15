// Windows-only test adapter. Does not certify Linux fcntl/exec/crash behaviour.
using System;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading;
class HostGuard {
    static string Quote(string value) {
        var b = new StringBuilder("\""); int slashes = 0;
        foreach (char c in value) {
            if (c == '\\') { slashes++; continue; }
            if (c == '"') { b.Append('\\', slashes * 2 + 1); b.Append(c); }
            else { b.Append('\\', slashes); b.Append(c); }
            slashes = 0;
        }
        b.Append('\\', slashes * 2); b.Append('"'); return b.ToString();
    }
    static int Main(string[] args) {
        if (Environment.GetEnvironmentVariable("BRORAY_OPS_TEST") != "1" || args.Length < 2) return 64;
        FileStream held = null;
        for (int i = 0; i < 200 && held == null; i++) {
            try { held = new FileStream(args[0], FileMode.OpenOrCreate, FileAccess.ReadWrite, FileShare.None); }
            catch (IOException) { Thread.Sleep(10); }
        }
        if (held == null) return 75;
        using (held) {
            var info = new ProcessStartInfo(args[1]); info.UseShellExecute = false; info.CreateNoWindow = true;
            info.RedirectStandardOutput = true; info.RedirectStandardError = true;
            info.StandardOutputEncoding = Encoding.UTF8; info.StandardErrorEncoding = Encoding.UTF8;
            var parts = new StringBuilder();
            for (int i = 2; i < args.Length; i++) { if (i > 2) parts.Append(' '); parts.Append(Quote(args[i])); }
            info.Arguments = parts.ToString(); info.EnvironmentVariables["BRORAY_OPS_GUARD_HELD"] = "1";
            using (var child = new Process()) {
                child.StartInfo = info;
                child.OutputDataReceived += delegate(object sender, DataReceivedEventArgs e) { if (e.Data != null) Console.Out.WriteLine(e.Data); };
                child.ErrorDataReceived += delegate(object sender, DataReceivedEventArgs e) { if (e.Data != null) Console.Error.WriteLine(e.Data); };
                child.Start(); child.BeginOutputReadLine(); child.BeginErrorReadLine();
                child.WaitForExit(); return child.ExitCode;
            }
        }
    }
}
