using System.Text.Json.Serialization;

namespace P2PChat.Frontend
{
    public class LogMessage
    {
        [JsonPropertyName("level")]
        public string Level { get; set; } = "";
        
        [JsonPropertyName("message")]
        public string Message { get; set; } = "";
    }
} 