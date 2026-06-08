import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import Ajv from "ajv";

const __dirname = dirname(fileURLToPath(import.meta.url));

const schema = JSON.parse(
  readFileSync(resolve(__dirname, "umpire.schema.json"), "utf-8")
);

const index = JSON.parse(
  readFileSync(resolve(__dirname, "conformance", "index.json"), "utf-8")
);

// --- Meta-schema: validate that umpire.schema.json is itself valid JSON Schema ---
{
  const metaAjv = new Ajv({ allErrors: true });
  const valid = metaAjv.validateSchema(schema);
  if (!valid) {
    console.log("umpire.schema.json is NOT valid JSON Schema draft-07:");
    for (const err of metaAjv.errors ?? []) {
      console.log(
        `  ${err.instancePath} ${err.message} (${JSON.stringify(err.params)})`
      );
    }
    process.exit(1);
  }
  console.log("✓ umpire.schema.json is valid JSON Schema draft-07\n");
}

const ajv = new Ajv({ allErrors: true, strict: false });
const validate = ajv.compile(schema);

let passed = 0;
let failed = 0;

function fixturePath(entryPath) {
  return resolve(__dirname, "conformance", entryPath);
}

function expectPass(name, data) {
  const valid = validate(data);
  if (valid) {
    console.log(`  ✓ ${name}`);
    passed++;
  } else {
    console.log(`  ✗ ${name} (expected to pass)`);
    for (const err of validate.errors ?? []) {
      console.log(
        `    ${err.instancePath} ${err.message} (${JSON.stringify(err.params)})`
      );
    }
    failed++;
  }
}

function expectFail(name, data) {
  const valid = validate(data);
  if (!valid) {
    console.log(`  ✓ ${name} (correctly rejected)`);
    passed++;
  } else {
    console.log(`  ✗ ${name} (expected to fail but passed)`);
    failed++;
  }
}

// --- Passing fixtures: validate the inner schema block ---
console.log("Passing fixtures:");
for (const entry of index.fixtures) {
  const fixture = JSON.parse(readFileSync(fixturePath(entry.path), "utf-8"));
  expectPass(entry.id, fixture.schema);
}

// --- Failure fixtures: schemas are structurally valid too ---
console.log("\nFailure fixtures:");
for (const entry of index.failures) {
  const fixture = JSON.parse(readFileSync(fixturePath(entry.path), "utf-8"));
  for (const failure of fixture.failures) {
    expectPass(`${entry.id} / ${failure.id}`, failure.schema);
  }
}

// --- Malformed: should be rejected ---
console.log("\nMalformed (expect rejection):");

expectFail("missing-fields", {
  version: 1,
  rules: [],
});

expectFail("missing-rules", {
  version: 1,
  fields: { x: {} },
});

expectFail("wrong-version", {
  version: 2,
  fields: { x: {} },
  rules: [],
});

expectFail("unknown-rule-type", {
  version: 1,
  fields: { x: {} },
  rules: [{ type: "nonsense" }],
});

expectFail("extra-property", {
  version: 1,
  fields: { x: {} },
  rules: [],
  extra: "nope",
});

expectFail("requires-missing-field", {
  version: 1,
  fields: { a: {} },
  rules: [{ type: "requires", dependency: "a" }],
});

expectFail("expr-unknown-op", {
  version: 1,
  fields: { a: {} },
  rules: [
    {
      type: "enabledWhen",
      field: "a",
      when: { op: "banana", field: "a" },
    },
  ],
});

expectFail("check-rule-no-op", {
  version: 1,
  fields: { a: {} },
  rules: [{ type: "check", field: "a" }],
});

// --- Report ---
console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
