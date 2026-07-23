declare module '@/lib/highlight.min.js' {
  const hljs: {
    highlight(code: string, options: { language: string }): { value: string };
    highlightAuto(code: string, languages?: string[]): { value: string; language: string };
    registerLanguage(name: string, definition: unknown): void;
    listLanguages(): string[];
  };
  export default hljs;
}

declare module '@/lib/highlight' {
  export { default } from './highlight.min.js';
}
