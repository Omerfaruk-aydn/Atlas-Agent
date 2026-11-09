"use strict";

// Reads the password export of Chrome, Edge and Firefox. The file holds
// secrets in plain text, so nothing here writes it anywhere or logs a row.

const { originOf } = require("./origin.cjs");

/** RFC 4180: quoted fields, doubled quotes, CRLF or LF, an optional BOM. */
function parseCsv(input) {
  const text = String(input).replace(/^﻿/, "");
  const rows = [];
  let row = [];
  let field = "";
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else quoted = false;
      } else field += c;
    } else if (c === '"' && field === "") quoted = true;
    else if (c === ",") {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && text[i + 1] === "\n") i++;
      row.push(field);
      field = "";
      if (row.some((x) => x !== "")) rows.push(row);
      row = [];
    } else field += c;
  }
  row.push(field);
  if (row.some((x) => x !== "")) rows.push(row);
  return rows;
}

/**
 * Logins from an export. Chrome and Edge write name,url,username,password,note;
 * Firefox writes url,username,password,httpRealm,formActionOrigin,... Rows that
 * are not an http(s) site with a password are counted, not imported.
 * @returns {{entries: {origin: string, username: string, password: string}[], skipped: number, format: string}}
 */
function parsePasswordCsv(input) {
  const rows = parseCsv(input);
  const head = (rows[0] ?? []).map((h) => h.trim().toLowerCase());
  const col = (name) => head.indexOf(name);
  const url = col("url");
  const username = col("username");
  const password = col("password");
  if (url < 0 || password < 0) return { entries: [], skipped: Math.max(0, rows.length - 1), format: "" };
  const format = col("httprealm") >= 0 ? "firefox" : col("name") >= 0 ? "chromium" : "csv";
  const entries = [];
  const seen = new Set();
  let skipped = 0;
  for (const row of rows.slice(1)) {
    const origin = originOf((row[url] ?? "").trim());
    const secret = row[password] ?? "";
    if (!origin || !secret) {
      skipped++;
      continue;
    }
    const user = username >= 0 ? (row[username] ?? "") : "";
    const key = `${origin}\n${user}`;
    if (seen.has(key)) {
      skipped++;
      continue;
    }
    seen.add(key);
    entries.push({ origin, username: user, password: secret });
  }
  return { entries, skipped, format };
}

module.exports = { parseCsv, parsePasswordCsv };
