using System.Net.Http;
using System.Net.Http.Headers;
using System.Text.Json;
using System.Diagnostics;

namespace P2PChat.Frontend {
    public class P2PManager {
        private Process? _goProcess;
        private readonly string _goExecutablePath;
        private readonly int _port;
        private static object _consoleLock = new object();
        private readonly HttpClient _httpClient;
        private readonly string _baseUrl;
        public P2PClient Client { get; }
        public Receiver Receiver { get; }

        public P2PManager(string goExecutablePath, int port, HttpClient httpClient, string baseUrl) {
            _goExecutablePath = goExecutablePath;
            _port = port;
            _httpClient = httpClient;
            _baseUrl = baseUrl;
            Client = new P2PClient(baseUrl, httpClient);
            Receiver = new Receiver(baseUrl, httpClient);
        }

        private async Task<bool> WaitForBackendStartupAsync() {
            int maxRetries = 20;
            int retryDelay = 1000; // 1 second

            Console.WriteLine("Waiting for Go backend to start...");
            
            for (int i = 0; i < maxRetries; i++) {
                // First check if the process is still running
                if (_goProcess == null || _goProcess.HasExited) {
                    var error = _goProcess?.StandardError.ReadToEnd() ?? "Process exited unexpectedly";
                    throw new Exception($"Go backend process failed: {error}");
                }

                try {
                    if (await Client.CheckStatus()) {
                        Console.WriteLine("Go backend is ready!");
                        return true;
                    }
                } catch (Exception) {
                    await Task.Delay(retryDelay);
                }
            }

            Console.WriteLine($"Backend failed to start after {maxRetries} attempts");
            return false;
        }

        public async Task StartBackend()
        {
            try
            {
                // Start the Go backend with the specified port
                _goProcess = new Process
                {
                    StartInfo = new ProcessStartInfo
                    {
                        FileName = _goExecutablePath,
                        Arguments = $"-port {_port}",
                        RedirectStandardOutput = true,
                        RedirectStandardError = true,
                        UseShellExecute = false,
                        CreateNoWindow = true,
                        Environment = { { "PORT", _port.ToString() } }
                    }
                };

                _goProcess.Start();

                // Wait for the backend to start up properly
                if (!await WaitForBackendStartupAsync())
                {
                    var error = await _goProcess.StandardError.ReadToEndAsync();
                    throw new Exception($"Go backend failed to start: {error}");
                }

                // Only start listening for logs after we confirm the backend is running
                _ = Receiver.StartLogStreamingAsync();
            }
            catch (Exception ex)
            {
                Console.ForegroundColor = ConsoleColor.Red;
                Console.WriteLine($"Failed to start Go backend: {ex.Message}");
                Console.ResetColor();
                throw;
            }
        }

        public async Task StopBackend()
        {
            Console.WriteLine("Stopping Go backend...");

            // Gracefully close the peer connection first, but only if backend is still running
            if (_goProcess != null && !_goProcess.HasExited)
            {
                try
                {
                    await Client.CloseConnection();
                }
                catch (Exception ex)
                {
                    Console.WriteLine($"{MessagePrefix.Debug} Could not close connection gracefully: {ex.Message}");
                }
            }

            if (_goProcess != null && !_goProcess.HasExited)
            {
                Receiver.StopLogStreaming();
                _goProcess.Kill();
                _goProcess.WaitForExit();
                Console.WriteLine("Go backend stopped");
            }
        }
    }
} 