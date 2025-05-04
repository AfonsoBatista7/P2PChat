namespace P2PChat.Frontend
{
    public enum LogLevel
    {
        /// <summary>
        /// Regular chat messages from peers
        /// </summary>
        Message,

        /// <summary>
        /// Informational messages about the system state
        /// </summary>
        Info,

        /// <summary>
        /// Error messages indicating something went wrong
        /// </summary>
        Error,

        /// <summary>
        /// Debug messages for development and troubleshooting
        /// </summary>
        Debug
    }
} 