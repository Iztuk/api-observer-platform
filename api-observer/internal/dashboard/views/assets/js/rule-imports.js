var __getOwnPropNames = Object.getOwnPropertyNames;
var __esm = (fn, res, err) => function __init() {
  if (err) throw err[0];
  try {
    return fn && (res = (0, fn[__getOwnPropNames(fn)[0]])(fn = 0)), res;
  } catch (e) {
    throw err = [e], e;
  }
};
var __commonJS = (cb, mod) => function __require() {
  try {
    return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
  } catch (e) {
    throw mod = 0, e;
  }
};

// internal/dashboard/views/scripts/toast.js
function showToast(html) {
  const container = document.getElementById("toast-container");
  if (!container) return;
  clearTimeout(toastTimeout);
  const doc = new DOMParser().parseFromString(html, "text/html");
  const toast = doc.querySelector(".app-toast");
  if (!toast) return;
  container.replaceChildren(toast);
  toast.querySelector(".app-toast-close")?.addEventListener("click", () => {
    clearTimeout(toastTimeout);
    container.replaceChildren();
  });
  toastTimeout = setTimeout(() => {
    container.replaceChildren();
  }, 5e3);
}
var toastTimeout;
var init_toast = __esm({
  "internal/dashboard/views/scripts/toast.js"() {
  }
});

// internal/dashboard/views/scripts/rule-imports.js
var require_rule_imports = __commonJS({
  "internal/dashboard/views/scripts/rule-imports.js"() {
    init_toast();
    async function translateImport() {
      const button = document.getElementById("translate-button");
      if (!button || button.disabled) return;
      const source = window.importEditor.getValue();
      const ruleType = document.getElementById("rule-type-selector").value;
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
      try {
        const response = await fetch("/rules/import/translate", {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            source,
            type: ruleType
          })
        });
        if (!response.ok) {
          const toastHTML = await response.text();
          showToast(toastHTML);
          return;
        }
        const translated = await response.text();
        window.translatedEditor.setValue(translated);
      } catch (error) {
        console.error("Translation failed:", error);
      } finally {
        button.disabled = false;
        button.removeAttribute("aria-busy");
      }
    }
    function initializeEditors() {
      if (!window.monaco) return;
      window.importEditor = window.monaco.editor.create(
        document.getElementById("src"),
        {
          value: "",
          language: "plaintext",
          automaticLayout: true
        }
      );
      window.translatedEditor = window.monaco.editor.create(
        document.getElementById("result"),
        {
          value: "",
          language: "yaml",
          automaticLayout: true
        }
      );
      document.getElementById("translate-button").addEventListener("click", translateImport);
    }
    if (window.monaco) {
      initializeEditors();
    } else {
      window.addEventListener("monaco:ready", initializeEditors, {
        once: true
      });
    }
  }
});
export default require_rule_imports();
