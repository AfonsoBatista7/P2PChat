using System.Net.Http;
using System.Net.Http.Headers;
using System.Text.Json;
using System.Diagnostics;

namespace P2PChat.Frontend
{
    public class GoBackendManager
    {
        private Process? _goProcess;
        private readonly string _goExecutablePath;
        private readonly int _port;
        private static object _consoleLock = new object();
        private readonly HttpClient _httpClient;
        private CancellationTokenSource? _logCancellationSource;

        public GoBackendManager(string goExecutablePath, int port)
        {
            _goExecutablePath = goExecutablePath;
            _port = port;
            _httpClient = new HttpClient();
        }

        private async Task HandleLogsAsync()
        {
            _logCancellationSource = new CancellationTokenSource();
            var token = _logCancellationSource.Token;

            while (!token.IsCancellationRequested)
            {
                try
                {
                    using (var request = new HttpRequestMessage(HttpMethod.Get, $"http://localhost:{_port}/api/logs"))
                    {
                        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("text/event-stream"));

                        using (var response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, token))
                        {
                            response.EnsureSuccessStatusCode();

                            using (var stream = await response.Content.ReadAsStreamAsync())
                            using (var reader = new StreamReader(stream))
                            {
                                while (!token.IsCancellationRequested)
                                {
                                    var line = await reader.ReadLineAsync();
                                    if (line == null)
                                    {
                                        lock (_consoleLock)
                                        {
                                            Console.ForegroundColor = ConsoleColor.Yellow;
                                            Console.WriteLine("[Debug] Log stream ended");
                                            Console.ResetColor();
                                        }
                                        break;
                                    }

                                    if (line.StartsWith("data: "))
                                    {
                                        var json = line.Substring(6);
                                        try
                                        {
                                            var logMessage = JsonSerializer.Deserialize<LogMessage>(json);

                                            if (logMessage != null)
                                            {
                                                lock (_consoleLock)
                                                {
                                                    Console.Write("\r"); // Move to start of line
                                                    Console.Write(new string(' ', Console.WindowWidth)); // Clear the line
                                                    Console.Write("\r"); // Move back to start
                                                    switch (logMessage.Level)
                                                    {
                                                        case "MSG":
                                                            // For chat messages, clear the current line and redraw
                                                            Console.WriteLine(logMessage.Message);
                                                            break;
                                                        case "ERROR":
                                                            Console.ForegroundColor = ConsoleColor.Red;
                                                            Console.WriteLine($"[Go Backend] {logMessage.Message}");
                                                            Console.ResetColor();
                                                            break;
                                                        case "INFO":
                                                            Console.ForegroundColor = ConsoleColor.Gray;
                                                            Console.WriteLine($"[Go Backend] {logMessage.Message}");
                                                            Console.ResetColor();
                                                            break;
                                                        default:
                                                            Console.ForegroundColor = ConsoleColor.Yellow;
                                                            Console.WriteLine($"[Go Backend] Unknown log level '{logMessage.Level}': {logMessage.Message}");
                                                            Console.ResetColor();
                                                            break;
                                                    }
                                                    Console.Write("> ");
                                                    Console.Write(ConsoleState.CurrentInput);
                                                }
                                            }
                                        }
                                        catch (JsonException ex)
                                        {
                                            lock (_consoleLock)
                                            {
                                                Console.ForegroundColor = ConsoleColor.Yellow;
                                                Console.WriteLine($"[Debug] Failed to parse log message: {json}");
                                                Console.WriteLine($"[Debug] Error: {ex.Message}");
                                                Console.ResetColor();
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                catch (Exception ex)
                {
                    if (!token.IsCancellationRequested)
                    {
                        lock (_consoleLock)
                        {
                            Console.ForegroundColor = ConsoleColor.Yellow;
                            Console.WriteLine($"[Log Error] {ex.Message}. Retrying in 1 second...");
                            Console.ResetColor();
                        }
                        await Task.Delay(1000, token);
                    }
                }
            }
        }

        private async Task<bool> WaitForBackendStartupAsync()
        {
            int maxRetries = 10;
            int retryDelay = 1000; // 1 second

            for (int i = 0; i < maxRetries; i++)
            {
                // First check if the process is still running
                if (_goProcess == null || _goProcess.HasExited) {
                    var error = _goProcess?.StandardError.ReadToEnd() ?? "Process exited unexpectedly";
                    throw new Exception($"Go backend process failed: {error}");
                }

                try {
                    using (var request = new HttpRequestMessage(HttpMethod.Get, $"http://localhost:{_port}/api/status"))
                    {
                        var response = await _httpClient.SendAsync(request);
                        if (response.IsSuccessStatusCode)
                        {
                            var content = await response.Content.ReadAsStringAsync();
                            lock (_consoleLock)
                            {
                                Console.ForegroundColor = ConsoleColor.Green;
                                Console.ResetColor();
                            }
                            return true;
                        }
                    }
                } catch (Exception ex) {
                    Console.WriteLine($"Error: {ex.Message}");
                    await Task.Delay(retryDelay);
                }
            }

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
                _ = HandleLogsAsync();
            }
            catch (Exception ex)
            {
                Console.ForegroundColor = ConsoleColor.Red;
                Console.WriteLine($"Failed to start Go backend: {ex.Message}");
                Console.ResetColor();
                throw;
            }
        }

        public void StopBackend()
        {
            Console.WriteLine("Stopping Go backend...");

            if (_goProcess != null && !_goProcess.HasExited)
            {
                _logCancellationSource?.Cancel();
                _goProcess.Kill();
                _goProcess.WaitForExit();
                Console.WriteLine("Go backend stopped");
            }
        }
    }
} 