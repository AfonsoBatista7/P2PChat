namespace P2PChat.Frontend {
    public static class ConsoleState {
        public static readonly object ConsoleLock = new object();
        public static string CurrentInput = "";
    }
} 