chrome.runtime.onMessage.addListener((message) => {
  if (message?.clicked) document.documentElement.setAttribute("data-atlas-ext-click", "yes");
});
