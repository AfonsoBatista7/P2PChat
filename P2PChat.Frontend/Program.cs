using System;
using System.Net.Http;
using System.Net.Http.Json;
using System.Text.Json;
using System.Threading.Tasks;
using System.Diagnostics;
using System.IO;
using System.Net.Http.Headers;
using System.Text.Json.Serialization;
using System.Text;

namespace P2PChat.Frontend {
    class Program {
        static async Task Main(string[] args) {

            int port = 8080; // Default port
            bool debug = false;
            string bootstrap = "";
            string goExecutablePath = Path.Combine(Directory.GetCurrentDirectory(), "go-code", "chatp2p");

            // Parse command line arguments
            for (int i = 0; i < args.Length; i++) {
                switch (args[i].ToLower()) {
                    case "-debug":
                        debug = true;
                        break;
                    case "-bootstrap":
                        if (i + 1 < args.Length) {
                            bootstrap = args[i + 1];
                            i++;
                        }
                        break;
                    case "-port":
                        if (i + 1 < args.Length && int.TryParse(args[i + 1], out int newPort)) {
                            port = newPort;
                            i++;
                        }
                        break;
                    case "-gopath":
                        if (i + 1 < args.Length) {
                            goExecutablePath = args[i + 1];
                            i++;
                        }
                        break;
                }
            }

            if (!File.Exists(goExecutablePath)) {
                Console.WriteLine($"Error: Go executable not found at {goExecutablePath}");
                Console.WriteLine("Please compile the Go code first using: go build -o chatp2p");
                return;
            }

            var goBackendManager = new GoBackendManager(goExecutablePath, port);
            var client = new P2PClient(port);
            string peerId = Guid.NewGuid().ToString();

            // Set up Ctrl+C handler
            Console.CancelKeyPress += async (sender, e) => {
                await client.CloseConnection();
                Environment.Exit(0);
            };

            try {
                // Start the Go backend
                await goBackendManager.StartBackend();

                Console.WriteLine($"Starting P2P client with ID: {peerId}");
                await client.StartP2P(peerId, bootstrap, debug);

                Console.WriteLine("Commands:");
                Console.WriteLine("  /connect <peer-id> - Connect to a peer");
                Console.WriteLine("  /status - Get current status");
                Console.WriteLine("  /exit - Quit the program");
                Console.WriteLine("----------------------------------------");
                Console.WriteLine("Just type and press Enter to send a message");
                Console.WriteLine("----------------------------------------");

                string input = "";
                while (true) {
                    lock (ConsoleState.ConsoleLock) {
                        Console.Write("> ");
                    }
                    input = "";
                    ConsoleState.CurrentInput = "";

                    while (true) {
                        if (Console.KeyAvailable) {
                            var key = Console.ReadKey(intercept: true);
                            if (key.Key == ConsoleKey.Enter) {
                                Console.WriteLine();
                                break;
                            } else if (key.Key == ConsoleKey.Backspace && input.Length > 0) {
                                input = input.Substring(0, input.Length - 1);
                                ConsoleState.CurrentInput = input;
                                Console.Write("\b \b");
                            } else if (!char.IsControl(key.KeyChar)) {
                                input += key.KeyChar;
                                ConsoleState.CurrentInput = input;
                                Console.Write(key.KeyChar);
                            }
                        } else {
                            await Task.Delay(50); // Avoid busy waiting
                        }
                    }

                    if (!string.IsNullOrEmpty(input)) {
                        if (input.ToLower().StartsWith("/connect")) {
                            string targetPeerId = input.Substring("/connect".Length).Trim();
                            await client.ConnectToPeer(targetPeerId);
                        } else if (input.ToLower().StartsWith("/status")) {
                            await client.GetStatus();
                        } else if (input.ToLower().StartsWith("/exit")) {
                            await client.CloseConnection();
                            break;
                        } else {
                            await client.SendMessage(input);
                        }
                    }
                }
            } catch (Exception ex) {
                Console.WriteLine($"Error: {ex.Message}");
                await client.CloseConnection();
            } finally {
                goBackendManager.StopBackend();
            }
        }
    }
}
