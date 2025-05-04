using System.Text.Json.Serialization;

namespace P2PChat.Frontend
{
    public class LogMessage
    {
        [JsonPropertyName("level")]
        public string Level { get; set; } = "";
        
        [JsonPropertyName("message")]
        public string Message { get; set; } = "";

        public LogLevel GetLogLevel()
        {
            return Level.ToUpper() switch
            {
                "MSG" => LogLevel.Message,
                "INFO" => LogLevel.Info,
                "ERROR" => LogLevel.Error,
                "DEBUG" => LogLevel.Debug,
                _ => LogLevel.Debug
            };
        }
    }
} 