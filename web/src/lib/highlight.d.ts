declare module '@/lib/highlight' {
  const hljs: {
    highlight(code: string, options: { language: string }): { value: string };
    highlightAuto(code: string, languages?: string[]): { value: string; language: string };
    getLanguage(name: string): { name: string; disableAutodetect?: boolean } | undefined;
    registerLanguage(name: string, definition: unknown): void;
    listLanguages(): string[];
  };
  export default hljs;
}
