import { showToast } from "./toast.js";

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
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        source,
        type: ruleType,
      }),
    });

    // The Go handler returns rendered Templ HTML on errors.
    if (!response.ok) {
      const toastHTML = await response.text();
      showToast(toastHTML);
      return;
    }

    // Successful responses contain YAML.
    const translated = await response.text();
    window.translatedEditor.setValue(translated);
  } catch (error) {
    // This handles network failures and other unexpected JS errors.
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
      automaticLayout: true,
    },
  );

  window.translatedEditor = window.monaco.editor.create(
    document.getElementById("result"),
    {
      value: "",
      language: "yaml",
      automaticLayout: true,
    },
  );

  document
    .getElementById("translate-button")
    .addEventListener("click", translateImport);
}

// Monaco may already be loaded.
if (window.monaco) {
  initializeEditors();
} else {
  window.addEventListener("monaco:ready", initializeEditors, {
    once: true,
  });
}
