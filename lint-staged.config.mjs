import { execFileSync, spawn } from "node:child_process";

const FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const SPINNER_MS = 80;

const color = (code) => (msg) => `\x1b[${code}m${msg}\x1b[0m`;
const green = color(32);
const red = color(31);

const run = (label, cmd, args = []) =>
  new Promise((resolve, reject) => {
    let i = 0;
    process.stdout.write("\r" + green(`${FRAMES[0]} ${label}`));

    const spinner = setInterval(() => {
      process.stdout.write("\r" + green(`${FRAMES[++i % FRAMES.length]} ${label}`));
    }, SPINNER_MS);

    const child = spawn(cmd, args);
    let stdout = "";

    child.stdout.on("data", (data) => {
      stdout += data.toString();
    });
    child.stderr.on("data", (data) => process.stderr.write(data.toString()));

    child.on("error", (err) => {
      clearInterval(spinner);
      process.stdout.write("\r" + red(`❌ ${label}\n`));
      reject(err);
    });

    child.on("exit", (code) => {
      clearInterval(spinner);
      if (code === 0) {
        process.stdout.write("\r" + green(`✅ ${label}\n`));
        resolve(stdout);
      } else {
        process.stdout.write("\r" + red(`❌ ${label}\n`));
        if (stdout) process.stderr.write(stdout);
        reject(new Error(`"${cmd} ${args.join(" ")}" exited with code ${code}`));
      }
    });
  });

// The stash ref before and after `git stash push` tells whether a stash was created.
// Popping when nothing was stashed would apply an older, unrelated stash.
const stashRef = () => {
  try {
    return execFileSync("git", ["rev-parse", "-q", "--verify", "refs/stash"], { encoding: "utf8" }).trim();
  } catch {
    return "";
  }
};

const checkFormat = async (files) => {
  const unformatted = (await run("Checking gofmt", "gofmt", ["-l", ...files])).trim();
  if (unformatted) {
    throw new Error(`gofmt: these files need formatting (run gofmt -w):\n${unformatted}`);
  }
};

export default {
  "**/*.go": async (files) => {
    if (!files?.length) return [];

    const before = stashRef();
    let stashed = false;

    try {
      await run("Stashing unstaged changes", "git", ["stash", "push", "--keep-index", "--include-untracked"]);
      stashed = stashRef() !== before;

      // The working tree now holds exactly what will be committed.
      await checkFormat(files);
      await run("Running golangci-lint", "golangci-lint", ["run", "./..."]);
      await run("Running tests", "make", ["test"]);
    } catch (err) {
      console.error(red(`🔄 ${err.message}`));
      process.exitCode = 1;
    } finally {
      if (stashed) {
        await run("Restoring unstaged changes", "git", ["stash", "pop"]).catch((err) => {
          console.error(red(`⚠️  ${err.message} — your changes are still in "git stash list"`));
          process.exitCode = 1;
        });
      }
    }

    if (process.exitCode) process.exit(process.exitCode);
    return [];
  },
};
