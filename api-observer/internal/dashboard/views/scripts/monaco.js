import * as monaco from "monaco-editor";

self.MonacoEnvironment = {
  getWorker() {
    return new Worker("/static/js/editor.worker.js", {
      type: "module",
    });
  },
};

function getMonacoTheme() {
  return document.documentElement.dataset.theme === "dark" ? "vs-dark" : "vs";
}

// Apply the theme when Monaco loads.
monaco.editor.setTheme(getMonacoTheme());

// Watch for changes made by your application's theme toggle.
const themeObserver = new MutationObserver(() => {
  monaco.editor.setTheme(getMonacoTheme());
});

themeObserver.observe(document.documentElement, {
  attributes: true,
  attributeFilter: ["data-theme"],
});

window.monaco = monaco;

window.dispatchEvent(new Event("monaco:ready"));
