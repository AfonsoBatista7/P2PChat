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

        public P2PClient(int port)
        {
            _baseUrl = $"http://localhost:{port}";
            _httpClient = new HttpClient();
        }

        public async Task StartP2P(string peerId, string bootstrap, bool debug)
        {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/start", new
            {
                peerId,
                bootstrap,
                debug
            });

            if (!response.IsSuccessStatusCode) 
                Console.WriteLine($"Failed to start P2P network: {await response.Content.ReadAsStringAsync()}");
        }

        public async Task ConnectToPeer(string peerId)
        {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/connect", new {
                peerId
            });

            if (!response.IsSuccessStatusCode) 
                Console.WriteLine($"Failed to connect to peer: {await response.Content.ReadAsStringAsync()}");
        }

        public async Task SendMessage(string message)
        {
            var response = await _httpClient.PostAsJsonAsync($"{_baseUrl}/api/send", new
            {
                message
            });

            if (!response.IsSuccessStatusCode) {
                Console.WriteLine($"Failed to send message: {await response.Content.ReadAsStringAsync()}");
            }
        }

        public async Task CloseConnection()
        {
            Console.WriteLine("Closing connection...");
            try {
                var content = new StringContent("{}", Encoding.UTF8, "application/json");
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/close", content);
                
                if (response.IsSuccessStatusCode) {
                    var responseContent = await response.Content.ReadAsStringAsync();
                    Console.WriteLine("Connection closed...");
                    Environment.Exit(0);
                } else {
                    var errorContent = await response.Content.ReadAsStringAsync();
                    Console.WriteLine($"Failed to close connection. Status: {response.StatusCode}, Content: {errorContent}");
                }
            } catch (Exception ex) {
                Console.WriteLine($"Exception while closing connection: {ex.Message}");
                Console.WriteLine($"Stack trace: {ex.StackTrace}");
                // Still exit even if there's an exception
                Environment.Exit(0);
            }
        }

        public async Task GetStatus()
        {
            var response = await _httpClient.GetAsync($"{_baseUrl}/api/status");
            if (response.IsSuccessStatusCode)
            {
                var content = await response.Content.ReadAsStringAsync();
                Console.WriteLine($"Status: {content}");
            }
            else
            {
                Console.WriteLine($"Failed to get status: {await response.Content.ReadAsStringAsync()}");
            }
        }
    }
} 