#!/usr/bin/env node

import readline from "node:readline";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { chromium } from "playwright-core";

const profileDir = process.env.AXON_ATTENTION_PROFILE ||
  path.join(os.homedir(), ".local", "share", "axon", "attention-browser");
const browserChannel = process.env.ATTENTION_BROWSER_CHANNEL || "chrome";
const executablePath = process.env.ATTENTION_BROWSER_EXECUTABLE || "";

const options = {
  headless: false,
  viewport: { width: 1440, height: 1000 },
};
if (executablePath) {
  options.executablePath = executablePath;
} else {
  options.channel = browserChannel;
}

const context = await chromium.launchPersistentContext(profileDir, options);
const pages = context.pages();
const linkedin = pages[0] || await context.newPage();
const gmail = await context.newPage();

await linkedin.goto("https://www.linkedin.com/messaging/", { waitUntil: "domcontentloaded" });
await gmail.goto("https://mail.google.com/mail/u/0/#inbox", { waitUntil: "domcontentloaded" });

console.log(`\nAxon attention browser profile: ${profileDir}`);
console.log("Log into LinkedIn and Gmail in the two browser tabs.");
console.log("Complete any MFA/challenge manually. This script does not automate login or bypass verification.");
console.log("When both accounts are ready, return here and press Enter to save the profile and close Chrome.\n");

const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
await new Promise((resolve) => rl.question("Press Enter when finished: ", resolve));
rl.close();

await context.close();
