using System.Net.Http;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;

namespace P2PChat.Frontend
{
    public class StatusResponse
    {
        public bool Success { get; set; }
        public string Message { get; set; } = "";
        public object? Error { get; set; } = null;
    }

    public class P2PClient
    {
        private readonly HttpClient _httpClient;
        private readonly string _baseUrl;
        private static object _consoleLock = new object();

        public P2PClient(string baseUrl, HttpClient httpClient) {
            _baseUrl = baseUrl;
            _httpClient = httpClient;
        }

        public async Task StartP2P(string peerId, string bootstrap, bool debug) {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/start", new {
                peerId,
                bootstrap,
                debug
            });

            if (!response.IsSuccessStatusCode) 
                Console.WriteLine($"{MessagePrefix.Error} Failed to start P2P network: {await response.Content.ReadAsStringAsync()}");
        }

        public async Task ConnectToPeer(string peerId) {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/connect", new {
                peerId
            });

            if (!response.IsSuccessStatusCode) 
                Console.WriteLine($"{MessagePrefix.Error} Failed to connect to peer: {await response.Content.ReadAsStringAsync()}");
        }

        public async Task SendMessage(string message) {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/send", new {
                message
            });

            if (!response.IsSuccessStatusCode) 
                Console.WriteLine($"{MessagePrefix.Error} Failed to send message: {await response.Content.ReadAsStringAsync()}");
            
        }

        public async Task CloseConnection() {

            Console.WriteLine($"{MessagePrefix.Status} Closing connection...");

            try {
                var content = new StringContent("{}", Encoding.UTF8, "application/json");
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/close", content);
                
                if (response.IsSuccessStatusCode) {
                    var responseContent = await response.Content.ReadAsStringAsync();

                    lock (ConsoleState.ConsoleLock) {
                        Console.WriteLine($"{MessagePrefix.Status} Connection closed...");
                    }
                } else {
                    var errorContent = await response.Content.ReadAsStringAsync();
                    lock (ConsoleState.ConsoleLock) {
                        Console.WriteLine($"{MessagePrefix.Error} Failed to close connection. Status: {response.StatusCode}, Content: {errorContent}");
                    }
                }
            } catch (Exception ex) {
                lock (ConsoleState.ConsoleLock) {
                    Console.WriteLine($"{MessagePrefix.Error} Exception while closing connection: {ex.Message}");
                    Console.WriteLine($"{MessagePrefix.Debug} Stack trace: {ex.StackTrace}");
                }
                // Do not exit here
            }
        }

        public async Task<bool> CheckStatus(bool printResult = false) {
            var response = await _httpClient.GetAsync($"{_baseUrl}/api/status");
            if (printResult) {
                if (response.IsSuccessStatusCode) {
                    var content = await response.Content.ReadAsStringAsync();
                    Console.WriteLine($"{MessagePrefix.Status} {content}");
                } else {
                    Console.WriteLine($"{MessagePrefix.Error} Failed to get status: {await response.Content.ReadAsStringAsync()}");
                }
            }
            return response.IsSuccessStatusCode;
        }

        public async Task<bool> HasPeerConnections() {
            try {
                var response = await _httpClient.GetAsync($"{_baseUrl}/api/status");
                if (response.IsSuccessStatusCode) {
                    var content = await response.Content.ReadAsStringAsync();
                    
                    // Try to parse the JSON response properly
                    try {
                        var statusResponse = JsonSerializer.Deserialize<StatusResponse>(content);
                        if (statusResponse?.Error != null) {
                            // The error field contains the status map as a string
                            // Format: "map[connected:true hasPeers:true peers:1]"
                            var statusString = statusResponse.Error?.ToString() ?? "";
                            if (statusString.Contains("hasPeers:true") || 
                                statusString.Contains("peers:1") || 
                                statusString.Contains("peers:2") || 
                                statusString.Contains("peers:3") || 
                                statusString.Contains("peers:4") || 
                                statusString.Contains("peers:5")) {
                                return true;
                            }
                        }
                    } catch (JsonException) {
                        // Fallback to string matching if JSON parsing fails
                        if (content.Contains("\"hasPeers\":true") || 
                            content.Contains("\"peers\":1") || 
                            content.Contains("\"peers\":2") || 
                            content.Contains("\"peers\":3") || 
                            content.Contains("\"peers\":4") || 
                            content.Contains("\"peers\":5")) {
                            return true;
                        }
                    }
                }
            } catch (Exception) {
                // If we can't check status, assume no connections
            }
            return false;
        }

                public async Task<bool> TriggerDiscovery() {
            try {
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/discover", new StringContent("{}", Encoding.UTF8, "application/json"));

                if (response.IsSuccessStatusCode) {
                    var content = await response.Content.ReadAsStringAsync();

                    // Parse the discovery response
                    try {
                        var statusResponse = JsonSerializer.Deserialize<StatusResponse>(content);
                        
                        // Try manual JSON parsing as fallback since Error field might not deserialize properly
                        if (statusResponse?.Error == null) {
                            // Check for peers in the raw content
                            if (content.Contains("hasPeers:true") || 
                                content.Contains("peers:1") || 
                                content.Contains("peers:2") || 
                                content.Contains("peers:3") || 
                                content.Contains("peers:4") || 
                                content.Contains("peers:5")) {
                                return true;
                            }
                        } else {
                            var statusString = statusResponse.Error?.ToString() ?? "";
                            if (statusString.Contains("hasPeers:true") || 
                                statusString.Contains("peers:1") || 
                                statusString.Contains("peers:2") || 
                                statusString.Contains("peers:3") || 
                                statusString.Contains("peers:4") || 
                                statusString.Contains("peers:5")) {
                                return true;
                            }
                        }
                    } catch (JsonException) {
                        // Fallback to string matching
                        if (content.Contains("\"hasPeers\":true") || 
                            content.Contains("\"peers\":1") || 
                            content.Contains("\"peers\":2") || 
                            content.Contains("\"peers\":3") || 
                            content.Contains("\"peers\":4") || 
                            content.Contains("\"peers\":5")) {
                            return true;
                        }
                    }
                }
            } catch (Exception) {
                // If discovery fails, assume no connections
            }

            return false;
        }
    }
} 