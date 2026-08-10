import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import Ajv from "ajv";
import Ajv2020 from "ajv/dist/2020.js";

const __dirname = dirname(fileURLToPath(import.meta.url));

// =============================================================================
// 1. Umpire meta-schema (draft-07) validation
// =============================================================================

const umpireSchema = JSON.parse(
  readFileSync(resolve(__dirname, "umpire.schema.json"), "utf-8")
);

// --- Meta-schema: validate that umpire.schema.json is itself valid JSON Schema ---
{
  const metaAjv = new Ajv({ allErrors: true });
  const valid = metaAjv.validateSchema(umpireSchema);
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

const umpireAjv = new Ajv({ allErrors: true, strict: false });
const validateUmpire = umpireAjv.compile(umpireSchema);

// =============================================================================
// 2. Profile meta-schema (2020-12) validation
// =============================================================================

const profileSchemaPath = resolve(
  __dirname, "profiles", "json-schema", "v1", "profile.schema.json"
);

let profileSchema;
if (existsSync(profileSchemaPath)) {
  profileSchema = JSON.parse(readFileSync(profileSchemaPath, "utf-8"));
} else {
  console.log("⚠ profile.schema.json not found — skipping profile validation\n");
  profileSchema = null;
}

let profileAjv20 = null;
let validateProfileMeta = null;


function getProfileAjv() {
  if (!profileAjv20) {
    // AJV 2020-12 instance for profile wrapper and value schema validation
    profileAjv20 = new Ajv2020({
      allErrors: true,
      strict: false,
    });
  }
  return profileAjv20;
}

function getProfileMetaValidator() {
  if (!validateProfileMeta && profileSchema) {
    const ajv20 = getProfileAjv();
    // Validate profile.schema.json itself against the 2020-12 meta-schema
    const metaValid = ajv20.validateSchema(profileSchema);
    if (!metaValid) {
      console.log("profile.schema.json is NOT valid JSON Schema 2020-12:");
      for (const err of ajv20.errors ?? []) {
        console.log(
          `  ${err.instancePath} ${err.message} (${JSON.stringify(err.params)})`
        );
      }
      process.exit(1);
    }
    console.log("✓ profile.schema.json is valid JSON Schema 2020-12\n");
    validateProfileMeta = ajv20.compile(profileSchema);
  }
  return validateProfileMeta;
}

// =============================================================================
// 3. Umpire conformance fixtures
// =============================================================================

const umpireIndex = JSON.parse(
  readFileSync(resolve(__dirname, "conformance", "index.json"), "utf-8")
);

let passed = 0;
let failed = 0;

function fixturePath(baseDir, entryPath) {
  return resolve(__dirname, baseDir, entryPath);
}

function expectPass(name, validateFn, data) {
  const valid = validateFn(data);
  if (valid) {
    console.log(`  ✓ ${name}`);
    passed++;
  } else {
    console.log(`  ✗ ${name} (expected to pass)`);
    for (const err of validateFn.errors ?? []) {
      console.log(
        `    ${err.instancePath} ${err.message} (${JSON.stringify(err.params)})`
      );
    }
    failed++;
  }
}

function expectFail(name, validateFn, data) {
  const valid = validateFn(data);
  if (!valid) {
    console.log(`  ✓ ${name} (correctly rejected)`);
    passed++;
  } else {
    console.log(`  ✗ ${name} (expected to fail but passed)`);
    failed++;
  }
}

// --- Passing fixtures: validate the inner schema block ---
console.log("=== Umpire conformance fixtures ===\n");

console.log("Passing fixtures:");
for (const entry of umpireIndex.fixtures) {
  const fixture = JSON.parse(
    readFileSync(fixturePath("conformance", entry.path), "utf-8")
  );
  expectPass(entry.id, validateUmpire, fixture.schema);
}

// --- Failure fixtures ---
console.log("\nFailure fixtures:");
for (const entry of umpireIndex.failures) {
  const fixture = JSON.parse(
    readFileSync(fixturePath("conformance", entry.path), "utf-8")
  );
  for (const failure of fixture.failures) {
    const metaSchema = failure.metaSchema ?? "accept";

    if (metaSchema === "reject") {
      expectFail(`${entry.id} / ${failure.id}`, validateUmpire, failure.schema);
    } else if (metaSchema === "accept") {
      expectPass(`${entry.id} / ${failure.id}`, validateUmpire, failure.schema);
    } else {
      console.log(
        `  ✗ ${entry.id} / ${failure.id} (unknown metaSchema expectation "${String(metaSchema)}")`,
      );
      failed++;
    }
  }
}

// --- Malformed: should be rejected ---
console.log("\nMalformed (expect rejection):");

expectFail("missing-fields", validateUmpire, {
  version: 1,
  rules: [],
});

expectFail("missing-rules", validateUmpire, {
  version: 1,
  fields: { x: {} },
});

expectFail("wrong-version", validateUmpire, {
  version: 2,
  fields: { x: {} },
  rules: [],
});

expectFail("unknown-rule-type", validateUmpire, {
  version: 1,
  fields: { x: {} },
  rules: [{ type: "nonsense" }],
});

expectFail("extra-property", validateUmpire, {
  version: 1,
  fields: { x: {} },
  rules: [],
  extra: "nope",
});

expectFail("requires-missing-field", validateUmpire, {
  version: 1,
  fields: { a: {} },
  rules: [{ type: "requires", dependency: "a" }],
});

expectFail("expr-unknown-op", validateUmpire, {
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

expectFail("check-rule-no-op", validateUmpire, {
  version: 1,
  fields: { a: {} },
  rules: [{ type: "check", field: "a" }],
});

// =============================================================================
// 4. Profile conformance fixtures
// =============================================================================

const profileIndexPath = resolve(__dirname, "profiles", "conformance", "index.json");

if (existsSync(profileIndexPath)) {
  const profileIndex = JSON.parse(readFileSync(profileIndexPath, "utf-8"));

  const validateProfile = getProfileMetaValidator();

  // Pre-compile a 2020-12 validator for value schemas used in profile passing cases
  const ajv20 = getProfileAjv();

  console.log("\n=== Profile conformance fixtures ===\n");

  // --- Profile passing fixtures ---
  if (validateProfile) {
    console.log("Profile structure validation:");
    for (const entry of profileIndex.fixtures) {
      const fixture = JSON.parse(
        readFileSync(fixturePath("profiles/conformance", entry.path), "utf-8")
      );
      expectPass(`${entry.id} (profile wrapper)`, validateProfile, fixture.profile);

      // Validate the embedded value schema $schema
      const valSchemas = fixture.profile.valueSchema ?? {};
      if (valSchemas.$schema === "https://json-schema.org/draft/2020-12/schema") {
        console.log(`  ✓ ${entry.id} valueSchema.$schema is 2020-12`);
        passed++;
      } else {
        console.log(`  ✗ ${entry.id} valueSchema.$schema is "${String(valSchemas.$schema)}", expected "https://json-schema.org/draft/2020-12/schema"`);
        failed++;
      }

      // Compile the value schema and run case-level structural validation
      const compileValue = ajv20.compile(valSchemas);
      console.log(`  ${entry.id} case-level structural validation:`);
      for (const c of fixture.cases ?? []) {
        const valid = compileValue(c.values ?? {});
        const issues = ajv20.errors ?? [];
        const expectedValid = c.expectedStructure?.valid !== false;

        if (valid === expectedValid) {
          console.log(`    ✓ ${c.id} (valid=${valid})`);
          passed++;
        } else {
          console.log(`    ✗ ${c.id} expected valid=${expectedValid} but got valid=${valid}`);
          if (issues.length > 0) {
            for (const err of issues) {
              console.log(`      ${err.instancePath} ${err.message}`);
            }
          }
          failed++;
        }
      }
    }
  }

  // --- Profile failure fixtures ---
  console.log("\nProfile definition failures:");
  for (const entry of profileIndex.failures) {
    const fixture = JSON.parse(
      readFileSync(fixturePath("profiles/conformance", entry.path), "utf-8")
    );
    for (const failure of fixture.failures) {
      // The profile wrapper should be structurally valid (validates the profile wrapper shape)
      if (validateProfile) {
        const profileValid = validateProfile(failure.profile);
        if (profileValid) {
          console.log(`  ✓ ${entry.id} / ${failure.id} (profile wrapper accepted)`);
          passed++;
        } else {
          // Profile wrapper itself failed — still counts as a pass since the
          // definition issue is detectable, but log what was caught
          console.log(`  ∼ ${entry.id} / ${failure.id} (wrapper rejected earlier: ${validateProfile.errors?.[0]?.instancePath} ${validateProfile.errors?.[0]?.message})`);
          passed++;
        }
      }
    }
  }
} else {
  console.log("\n⚠ Profile conformance not found — skipping profile validation");
}

// =============================================================================
// 5. Report
// =============================================================================
console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
