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
            string goExecutablePath = Path.Combine(Directory.GetCurrentDirectory(), "P2PChat.LibP2P", "chatp2p");

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

            var httpClient = new HttpClient();
            string baseUrl = $"http://localhost:{port}";
            var p2pManager = new P2PManager(goExecutablePath, port, httpClient, baseUrl);
            string peerId = Guid.NewGuid().ToString();

            // Subscribe to log messages from the backend
            p2pManager.Receiver.LogReceived += (logMessage) => {
                lock (ConsoleState.ConsoleLock) {
                    Console.Write("\r"); // Move to start of line
                    Console.Write(new string(' ', Console.WindowWidth)); // Clear the line
                    Console.Write("\r"); // Move back to start
                    
                    switch (logMessage.GetLogLevel()) {
                        case LogLevel.Message:
                            Console.WriteLine(logMessage.Message);
                            break;
                        case LogLevel.Error:
                            Console.ForegroundColor = ConsoleColor.Red;
                            Console.WriteLine($"{MessagePrefix.GoBackend} {logMessage.Message}");
                            Console.ResetColor();
                            break;
                        case LogLevel.Info:
                            Console.ForegroundColor = ConsoleColor.Gray;
                            Console.WriteLine($"{MessagePrefix.GoBackend} {logMessage.Message}");
                            Console.ResetColor();
                            break;
                        case LogLevel.Debug:
                            Console.ForegroundColor = ConsoleColor.Yellow;
                            Console.WriteLine($"{MessagePrefix.GoBackend} {logMessage.Message}");
                            Console.ResetColor();
                            break;
                    }
                    Console.Write("> ");
                    Console.Write(ConsoleState.CurrentInput);
                }
            };

            // Set up Ctrl+C handler
            Console.CancelKeyPress += async (sender, e) => {
                await p2pManager.Client.CloseConnection();
                Environment.Exit(0);
            };

            try {
                // Start the Go backend
                await p2pManager.StartBackend();

                Console.WriteLine($"Starting P2P client with ID: {peerId}");
                await p2pManager.Client.StartP2P(peerId, bootstrap, debug);

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
                            await p2pManager.Client.ConnectToPeer(targetPeerId);
                        } else if (input.ToLower().StartsWith("/status")) {
                            await p2pManager.Client.CheckStatus(true);
                        } else if (input.ToLower().StartsWith("/exit")) {
                            break;
                        } else {
                            await p2pManager.Client.SendMessage(input);
                        }
                    }
                }
            } catch (Exception ex) {
                Console.WriteLine($"Error: {ex.Message}");
                await p2pManager.Client.CloseConnection();
            } finally {
                await p2pManager.StopBackend();
            }
        }
    }
}
