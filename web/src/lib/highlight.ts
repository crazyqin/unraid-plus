// highlight.js v11.9.0 - re-export for convenience
// The UMD bundle exports via `module.exports = hljs`, so we use
// `import * as` to let Vite/Rollup handle CJS interop, then extract default.
import * as hljsModule from './highlight.min.js';
const hljs = (hljsModule as any).default ?? hljsModule;
export default hljs;
