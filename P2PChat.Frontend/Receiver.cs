using System;
using System.IO;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Threading;
using System.Threading.Tasks;
using System.Text.Json;

namespace P2PChat.Frontend {
    public class Receiver {
        private readonly HttpClient _httpClient;
        private readonly string _baseUrl;
        private CancellationTokenSource? _cts;

        // Event for log messages
        public event Action<LogMessage>? LogReceived;

        public Receiver(string baseUrl, HttpClient httpClient) {
            _baseUrl = baseUrl;
            _httpClient = httpClient;
        }

        // Start streaming logs from the backend
        public async Task StartLogStreamingAsync() {
            _cts = new CancellationTokenSource();
            var token = _cts.Token;

            while (!token.IsCancellationRequested) {
                try {
                    using (var request = new HttpRequestMessage(HttpMethod.Get, $"{_baseUrl}/api/logs")) {
                        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("text/event-stream"));
                        using (var response = await _httpClient.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, token)) {
                            response.EnsureSuccessStatusCode();
                            using (var stream = await response.Content.ReadAsStreamAsync())
                            using (var reader = new StreamReader(stream)) {
                                while (!token.IsCancellationRequested) {
                                    var line = await reader.ReadLineAsync();
                                    if (line == null) break;
                                    if (line.StartsWith("data: ")) {
                                        var json = line.Substring(6);
                                        try {
                                            var logMessage = JsonSerializer.Deserialize<LogMessage>(json);
                                            if (logMessage != null) {
                                                LogReceived?.Invoke(logMessage);
                                            }
                                        } catch (JsonException) {
                                            // Optionally handle parse errors
                                        }
                                    }
                                }
                            }
                        }
                    }
                } catch (Exception) {
                    if (!token.IsCancellationRequested) {
                        await Task.Delay(1000, token); // Retry after delay
                    }
                }
            }
        }

        public void StopLogStreaming() {
            _cts?.Cancel();
        }
    }
} 