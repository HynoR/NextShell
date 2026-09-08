// Vite `?raw` imports for non-code assets loaded from the Electron main
// bundle (e.g. the shell-integration scripts). tsconfig.web gets these from
// `vite/client`; tsconfig.node does not include vite types, so declare the
// exact suffixes used by main-process code here.
declare module "*.sh?raw" {
  const content: string;
  export default content;
}

declare module "*.bash?raw" {
  const content: string;
  export default content;
}

declare module "*.zsh?raw" {
  const content: string;
  export default content;
}

declare module "*.fish?raw" {
  const content: string;
  export default content;
}

declare module "*.md?raw" {
  const content: string;
  export default content;
}
