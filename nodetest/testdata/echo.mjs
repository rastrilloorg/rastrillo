// Upcases stdin, and echoes TZ and its own arguments, so one run proves
// stdin, Env, Args and stdout capture.
const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
process.stdout.write(Buffer.concat(chunks).toString("utf8").toUpperCase());
process.stdout.write(` tz=${process.env.TZ ?? ""} args=${process.argv.slice(2).join(",")}`);
