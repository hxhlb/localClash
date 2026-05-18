#!/usr/bin/env node
import fs from "node:fs";
import process from "node:process";

const DEFAULT_CDP_URL = process.env.CHERRY_STUDIO_CDP_URL || "http://127.0.0.1:9333";
const DEFAULT_TIMEOUT_MS = 10000;

function usage() {
  return `Usage: rtk node scripts/cherry_latest_topic_jsonl.mjs [options]

Outputs JSONL for the latest Cherry Studio UI chat topic through Cherry Studio's
own Chrome DevTools Protocol endpoint. Records keep raw IndexedDB objects,
including raw tool-call/tool-result message_blocks.

Cherry Studio must be launched with remote debugging enabled, for example:
  rtk open -na "Cherry Studio" --args --remote-debugging-port=9333

Options:
  --cdp <url>          Cherry Studio CDP HTTP URL.
                       Default: ${DEFAULT_CDP_URL}
  --topic-id <id>      Export a specific topic instead of the latest one.
  --target-id <id>     Use a specific CDP target id.
  --target-url <text>  Use the first target whose URL contains this text.
  --output <path>      Write JSONL to a file instead of stdout.
  --include-empty      Allow selecting a topic with no messages.
  --list-targets       Print CDP targets as JSONL and exit.
  --timeout-ms <n>     CDP request timeout. Default: ${DEFAULT_TIMEOUT_MS}.
  -h, --help           Show this help.

Examples:
  rtk node scripts/cherry_latest_topic_jsonl.mjs --cdp http://127.0.0.1:9333
  rtk node scripts/cherry_latest_topic_jsonl.mjs --output /tmp/cherry-latest.jsonl
`;
}

function parseArgs(argv) {
  const args = {
    cdp: DEFAULT_CDP_URL,
    topicId: "",
    targetId: "",
    targetUrl: "",
    output: "",
    includeEmpty: false,
    listTargets: false,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    help: false,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const readValue = () => {
      i += 1;
      if (i >= argv.length) throw new Error(`${arg} requires a value`);
      return argv[i];
    };

    switch (arg) {
      case "--cdp":
        args.cdp = readValue();
        break;
      case "--topic-id":
        args.topicId = readValue();
        break;
      case "--target-id":
        args.targetId = readValue();
        break;
      case "--target-url":
        args.targetUrl = readValue();
        break;
      case "--output":
        args.output = readValue();
        break;
      case "--include-empty":
        args.includeEmpty = true;
        break;
      case "--list-targets":
        args.listTargets = true;
        break;
      case "--timeout-ms":
        args.timeoutMs = Number(readValue());
        if (!Number.isFinite(args.timeoutMs) || args.timeoutMs <= 0) {
          throw new Error("--timeout-ms must be a positive number");
        }
        break;
      case "-h":
      case "--help":
        args.help = true;
        break;
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }

  args.cdp = normalizeCdpUrl(args.cdp);
  return args;
}

function normalizeCdpUrl(value) {
  const url = new URL(value);
  url.hash = "";
  url.search = "";
  return url.toString().replace(/\/$/, "");
}

async function fetchJson(url, timeoutMs) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(url, { signal: controller.signal });
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    return await response.json();
  } finally {
    clearTimeout(timer);
  }
}

function jsonLine(value) {
  return `${JSON.stringify(value)}\n`;
}

function writeJsonl(records, outputPath) {
  const payload = records.map(jsonLine).join("");
  if (outputPath) fs.writeFileSync(outputPath, payload);
  else process.stdout.write(payload);
}

class CdpClient {
  constructor(webSocketUrl, timeoutMs) {
    this.webSocketUrl = webSocketUrl;
    this.timeoutMs = timeoutMs;
    this.nextId = 1;
    this.pending = new Map();
    this.socket = null;
  }

  async connect() {
    this.socket = new WebSocket(this.webSocketUrl);
    this.socket.addEventListener("message", (event) => this.onMessage(event));
    this.socket.addEventListener("close", () => this.rejectAll(new Error("CDP socket closed")));
    this.socket.addEventListener("error", () => this.rejectAll(new Error("CDP socket error")));
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timed out connecting to CDP target")), this.timeoutMs);
      this.socket.addEventListener("open", () => {
        clearTimeout(timer);
        resolve();
      }, { once: true });
      this.socket.addEventListener("error", () => {
        clearTimeout(timer);
        reject(new Error("failed to connect to CDP target"));
      }, { once: true });
    });
  }

  onMessage(event) {
    let message;
    try {
      message = JSON.parse(event.data);
    } catch {
      return;
    }
    if (!message.id || !this.pending.has(message.id)) return;
    const { resolve, reject, timer } = this.pending.get(message.id);
    clearTimeout(timer);
    this.pending.delete(message.id);
    if (message.error) reject(new Error(`${message.error.message}: ${message.error.data || ""}`.trim()));
    else resolve(message.result);
  }

  async send(method, params = {}) {
    const id = this.nextId;
    this.nextId += 1;
    const payload = JSON.stringify({ id, method, params });
    const promise = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`CDP ${method} timed out`));
      }, this.timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
    });
    this.socket.send(payload);
    return await promise;
  }

  rejectAll(error) {
    for (const { reject, timer } of this.pending.values()) {
      clearTimeout(timer);
      reject(error);
    }
    this.pending.clear();
  }

  close() {
    if (this.socket) this.socket.close();
  }
}

async function evaluateInTarget(target, expression, timeoutMs) {
  const client = new CdpClient(target.webSocketDebuggerUrl, timeoutMs);
  await client.connect();
  try {
    await client.send("Runtime.enable");
    const result = await client.send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
      timeout: timeoutMs,
    });
    if (result.exceptionDetails) {
      const details = result.exceptionDetails;
      const text = details.exception?.description || details.text || "evaluation failed";
      throw new Error(text);
    }
    return result.result?.value;
  } finally {
    client.close();
  }
}

async function getTargets(cdpUrl, timeoutMs) {
  try {
    await fetchJson(`${cdpUrl}/json/version`, timeoutMs);
    const targets = await fetchJson(`${cdpUrl}/json/list`, timeoutMs);
    return targets.filter((target) => target.webSocketDebuggerUrl);
  } catch (error) {
    throw new Error(
      `Cherry Studio CDP endpoint is not available at ${cdpUrl}. ` +
        "Relaunch Cherry Studio with: rtk open -na \"Cherry Studio\" --args --remote-debugging-port=9333. " +
        `Original error: ${error.message}`,
    );
  }
}

async function selectTarget(args, targets) {
  if (args.targetId) {
    const target = targets.find((item) => item.id === args.targetId);
    if (!target) throw new Error(`CDP target not found: ${args.targetId}`);
    return target;
  }

  if (args.targetUrl) {
    const target = targets.find((item) => (item.url || "").includes(args.targetUrl));
    if (!target) throw new Error(`CDP target URL did not match: ${args.targetUrl}`);
    return target;
  }

  const likelyTargets = targets.filter((target) => {
    const type = target.type || "";
    return type === "page" || type === "webview" || type === "background_page";
  });

  for (const target of likelyTargets) {
    try {
      const probe = await evaluateInTarget(target, CHERRY_DB_PROBE_EXPRESSION, args.timeoutMs);
      if (probe?.hasCherryStudioDb) return target;
    } catch {
      // Some Electron targets cannot evaluate page JavaScript. Keep probing.
    }
  }

  throw new Error(
    "no CDP target exposes IndexedDB database CherryStudio; use --list-targets and pass --target-id",
  );
}

const CHERRY_DB_PROBE_EXPRESSION = `(${function probeCherryDb() {
  return (async () => {
    if (!globalThis.indexedDB || !indexedDB.databases) {
      return { hasCherryStudioDb: false, reason: "indexedDB.databases unavailable" };
    }
    const databases = await indexedDB.databases();
    return {
      hasCherryStudioDb: databases.some((database) => database.name === "CherryStudio"),
      databases,
      location: globalThis.location?.href || "",
    };
  })();
}.toString()})()`;

function cherryDbReadExpression(args) {
  return `(${readCherryDbInPage.toString()})(${JSON.stringify({
    topicId: args.topicId,
    includeEmpty: args.includeEmpty,
  })})`;
}

function readCherryDbInPage(options) {
  return (async ({ topicId, includeEmpty }) => {
    function cloneForJson(value, seen = new WeakSet()) {
      if (value === undefined) return { __cherry_jsonl_type: "undefined" };
      if (value === null || typeof value === "string" || typeof value === "boolean") return value;
      if (typeof value === "number") return Number.isFinite(value) ? value : String(value);
      if (typeof value === "bigint") return value.toString();
      if (value instanceof Date) return value.toISOString();
      if (value instanceof ArrayBuffer) {
        const bytes = new Uint8Array(value);
        let binary = "";
        for (const byte of bytes) binary += String.fromCharCode(byte);
        return { __cherry_jsonl_type: "ArrayBuffer", base64: btoa(binary), byteLength: bytes.byteLength };
      }
      if (ArrayBuffer.isView(value)) {
        return {
          __cherry_jsonl_type: value.constructor.name,
          value: cloneForJson(Array.from(value), seen),
        };
      }
      if (value instanceof Blob) {
        return {
          __cherry_jsonl_type: "Blob",
          size: value.size,
          type: value.type,
        };
      }
      if (Array.isArray(value)) return value.map((item) => cloneForJson(item, seen));
      if (value instanceof Map) {
        return {
          __cherry_jsonl_type: "Map",
          entries: Array.from(value.entries()).map(([key, item]) => [cloneForJson(key, seen), cloneForJson(item, seen)]),
        };
      }
      if (value instanceof Set) {
        return {
          __cherry_jsonl_type: "Set",
          values: Array.from(value.values()).map((item) => cloneForJson(item, seen)),
        };
      }
      if (typeof value === "object") {
        if (seen.has(value)) return { __cherry_jsonl_type: "circular" };
        seen.add(value);
        const output = {};
        for (const [key, item] of Object.entries(value)) output[key] = cloneForJson(item, seen);
        seen.delete(value);
        return output;
      }
      return String(value);
    }

    function promisify(request) {
      return new Promise((resolve, reject) => {
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
    }

    function txDone(tx) {
      return new Promise((resolve, reject) => {
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
        tx.onabort = () => reject(tx.error || new Error("transaction aborted"));
      });
    }

    function openDb(name) {
      return new Promise((resolve, reject) => {
        const request = indexedDB.open(name);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
        request.onupgradeneeded = () => {
          request.transaction.abort();
          reject(new Error(`unexpected upgrade for IndexedDB ${name}`));
        };
      });
    }

    function parseTime(value) {
      if (!value) return 0;
      const parsed = Date.parse(value);
      return Number.isFinite(parsed) ? parsed : 0;
    }

    function topicSortTime(topic) {
      let latest = Math.max(parseTime(topic.updatedAt), parseTime(topic.createdAt));
      for (const message of topic.messages || []) {
        latest = Math.max(latest, parseTime(message.updatedAt), parseTime(message.createdAt));
      }
      return latest;
    }

    async function getAll(db, storeName) {
      const tx = db.transaction(storeName, "readonly");
      const values = await promisify(tx.objectStore(storeName).getAll());
      await txDone(tx);
      return values;
    }

    async function getBlocksByIds(db, blockIds) {
      if (!blockIds.length) return [];
      const tx = db.transaction("message_blocks", "readonly");
      const store = tx.objectStore("message_blocks");
      const blocks = [];
      for (const id of blockIds) {
        const block = await promisify(store.get(id));
        if (block) blocks.push(block);
      }
      await txDone(tx);
      return blocks;
    }

    async function getBlocksByMessageIds(db, messageIds, knownBlockIds) {
      if (!messageIds.length) return [];
      const tx = db.transaction("message_blocks", "readonly");
      const store = tx.objectStore("message_blocks");
      const blocks = [];
      if (store.indexNames.contains("messageId")) {
        const index = store.index("messageId");
        for (const messageId of messageIds) {
          const found = await promisify(index.getAll(IDBKeyRange.only(messageId)));
          blocks.push(...found);
        }
      } else {
        const allBlocks = await promisify(store.getAll());
        blocks.push(...allBlocks.filter((block) => messageIds.includes(block.messageId)));
      }
      await txDone(tx);
      return blocks.filter((block) => !knownBlockIds.has(block.id));
    }

    const db = await openDb("CherryStudio");
    const stores = Array.from(db.objectStoreNames);
    if (!stores.includes("topics") || !stores.includes("message_blocks")) {
      throw new Error(`unexpected CherryStudio stores: ${stores.join(", ")}`);
    }

    const topics = await getAll(db, "topics");
    const candidates = topics
      .filter((topic) => topicId ? topic.id === topicId : true)
      .filter((topic) => includeEmpty || (topic.messages || []).length > 0)
      .sort((a, b) => topicSortTime(b) - topicSortTime(a));

    const topic = candidates[0];
    if (!topic) throw new Error(topicId ? `topic not found: ${topicId}` : "no non-empty topic found");

    const messages = topic.messages || [];
    const blockIds = messages.flatMap((message) => message.blocks || []);
    const blocksFromIds = await getBlocksByIds(db, blockIds);
    const extraBlocks = await getBlocksByMessageIds(
      db,
      messages.map((message) => message.id),
      new Set(blocksFromIds.map((block) => block.id)),
    );
    const blocks = [...blocksFromIds, ...extraBlocks];
    const blockById = new Map(blocks.map((block) => [block.id, block]));
    const orderedBlocks = [];
    const seen = new Set();

    for (const message of messages) {
      for (const blockId of message.blocks || []) {
        const block = blockById.get(blockId);
        if (block && !seen.has(block.id)) {
          orderedBlocks.push(block);
          seen.add(block.id);
        }
      }
      for (const block of blocks.filter((item) => item.messageId === message.id)) {
        if (!seen.has(block.id)) {
          orderedBlocks.push(block);
          seen.add(block.id);
        }
      }
    }

    db.close();
    return cloneForJson({
      location: globalThis.location?.href || "",
      stores,
      topic,
      messages,
      blocks: orderedBlocks,
      topicCount: topics.length,
      selectedTopicSortTime: topicSortTime(topic),
    });
  })(options);
}

function buildRecords(data, source) {
  const exportedAt = new Date().toISOString();
  const topic = data.topic;
  const messages = data.messages || [];
  const blocks = data.blocks || [];
  const blocksByMessage = new Map();

  for (const block of blocks) {
    const list = blocksByMessage.get(block.messageId) || [];
    list.push(block);
    blocksByMessage.set(block.messageId, list);
  }

  const records = [{
    type: "metadata",
    exportedAt,
    source: "CherryStudio CDP IndexedDB",
    cdp: source.cdp,
    target: source.target,
    rendererLocation: data.location,
    topicCount: data.topicCount,
    objectStores: data.stores,
    selectedTopic: {
      id: topic.id,
      name: topic.name,
      assistantId: topic.assistantId,
      createdAt: topic.createdAt,
      updatedAt: topic.updatedAt,
      messageCount: messages.length,
      blockCount: blocks.length,
    },
  }, {
    type: "topic",
    topicId: topic.id,
    raw: topic,
  }];

  for (const message of messages) {
    records.push({
      type: "message",
      topicId: topic.id,
      messageId: message.id,
      role: message.role,
      createdAt: message.createdAt,
      status: message.status,
      raw: message,
    });

    for (const block of blocksByMessage.get(message.id) || []) {
      records.push({
        type: "message_block",
        topicId: topic.id,
        messageId: message.id,
        blockId: block.id,
        blockType: block.type,
        createdAt: block.createdAt,
        status: block.status,
        raw: block,
      });
    }
  }

  return records;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    process.stdout.write(usage());
    return;
  }

  const targets = await getTargets(args.cdp, args.timeoutMs);
  if (args.listTargets) {
    writeJsonl(targets.map((target) => ({
      type: "target",
      id: target.id,
      targetType: target.type,
      title: target.title,
      url: target.url,
      attached: target.attached,
    })), args.output);
    return;
  }

  const target = await selectTarget(args, targets);
  const data = await evaluateInTarget(target, cherryDbReadExpression(args), args.timeoutMs);
  const records = buildRecords(data, {
    cdp: args.cdp,
    target: {
      id: target.id,
      targetType: target.type,
      title: target.title,
      url: target.url,
    },
  });
  writeJsonl(records, args.output);
}

main().catch((error) => {
  process.stderr.write(`error: ${error.message}\n`);
  process.exit(1);
});
