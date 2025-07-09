using System.Net.Http;
using System.Net.Http.Json;
using System.Text;

namespace P2PChat.Frontend
{
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
                    Console.WriteLine($"{MessagePrefix.Status} Connection closed...");
                } else {
                    var errorContent = await response.Content.ReadAsStringAsync();
                    Console.WriteLine($"{MessagePrefix.Error} Failed to close connection. Status: {response.StatusCode}, Content: {errorContent}");
                }
            } catch (Exception ex) {
                Console.WriteLine($"{MessagePrefix.Error} Exception while closing connection: {ex.Message}");
                Console.WriteLine($"{MessagePrefix.Debug} Stack trace: {ex.StackTrace}");
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
    }
} 