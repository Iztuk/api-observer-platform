let toastTimeout;

export function showToast(html) {
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
  }, 5000);
}
