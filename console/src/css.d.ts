// Stylesheets imported for their side effect.
//
// Vite turns `import './design/styles.css'` into a link tag; there is no module
// and nothing is bound. TypeScript 6 stopped assuming that (TS2882) and now
// wants a declaration for the import, so this is it.
declare module '*.css';
