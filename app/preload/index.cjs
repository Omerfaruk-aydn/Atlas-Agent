"use strict";

// window.atlas: the narrow desktop API. Sandboxed, so only `electron` is
// available here. See main/ipc.cjs for what each call does.

const { contextBridge, ipcRenderer } = require("electron");

function listen(channel, cb) {
  const handler = (_event, value) => cb(value);
  ipcRenderer.on(channel, handler);
  return () => ipcRenderer.removeListener(channel, handler);
}

contextBridge.exposeInMainWorld("atlas", {
  boot: () => ipcRenderer.sendSync("atlas:boot"),
  appInfo: () => ipcRenderer.invoke("atlas:app-info"),
  setTheme: (theme) => ipcRenderer.invoke("atlas:theme-set", theme),
  onTheme: (cb) => listen("atlas:theme", cb),
  onMaximized: (cb) => listen("atlas:maximized", cb),
  setMotion: (motion) => ipcRenderer.invoke("atlas:motion-set", motion),
  setUI: (key, value) => ipcRenderer.invoke("atlas:ui-set", key, value),
  gitStatus: (project) => ipcRenderer.invoke("atlas:git-status", project),
  favicon: (host) => ipcRenderer.invoke("atlas:favicon", host),
  notify: (note) => ipcRenderer.invoke("atlas:notify", note),
  setBadge: (count, image) => ipcRenderer.invoke("atlas:badge", count, image),
  onOpenSession: (cb) => listen("atlas:open-session", cb),
  pickFolder: () => ipcRenderer.invoke("atlas:pick-folder"),
  openProject: (project) => ipcRenderer.invoke("atlas:open-project", project),
  openExternal: (url) => ipcRenderer.invoke("atlas:open-external", url),
  reveal: (target) => ipcRenderer.invoke("atlas:reveal", target),
});
