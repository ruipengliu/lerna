// Lerna schema generator v1.0.0. No unsupported schema keyword is ignored.
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
const prettierCLI = fileURLToPath(
  new URL('./bin/prettier.cjs', import.meta.resolve('prettier')),
);
import { execFileSync } from 'node:child_process';
const configurations = [
  {
    version: '1.0.0',
    source: 'contract/schema/1.0.0/values.json',
    go: 'contract/gen/go/values.go',
    aliases: 'contract/values.go',
    namespace: 'contract',
    goImport: 'github.com/ruipengliu/lerna/contract/gen/go',
    contractImport: 'github.com/ruipengliu/lerna/contract',
    ts: 'sdk/typescript/src/generated/values.ts',
    runner: 'conformance/component/valuerunner/types.go',
  },
  {
    version: '1.1.0',
    source: 'contract/schema/1.1.0/values.json',
    go: 'contract/gen/go/v1_1/values.go',
    aliases: 'contract/v1_1/values.go',
    namespace: 'v1_1',
    goImport: 'github.com/ruipengliu/lerna/contract/gen/go/v1_1',
    contractImport: 'github.com/ruipengliu/lerna/contract/v1_1',
    ts: 'sdk/typescript/src/v1_1/generated/values.ts',
    runner: 'conformance/component/valuerunner_v1_1/types.go',
  },
];
for (const configuration of configurations) {
  const source = configuration.source;

  const schema = JSON.parse(readFileSync(source, 'utf8'));
  const allowed = new Set([
    '$schema',
    '$id',
    '$defs',
    '$ref',
    'type',
    'properties',
    'required',
    'additionalProperties',
    'items',
    'maxItems',
    'maxProperties',
    'minItems',
    'pattern',
    'format',
    'minLength',
    'maxLength',
    'enum',
    'description',
    'const',
    'allOf',
    'oneOf',
    'if',
    'then',
    'else',
  ]);
  function audit(node, path = []) {
    for (const [key, value] of Object.entries(node)) {
      if (
        typeof value === 'number' &&
        (!Number.isSafeInteger(value) || value < 0 || Object.is(value, -0))
      )
        throw Error('unsafe schema number');
      if (!allowed.has(key)) throw Error(`unsupported schema keyword: ${key}`);
      if (['properties', '$defs'].includes(key))
        Object.entries(value).forEach(([name, child]) =>
          audit(child, [...path, key, name]),
        );
      if (['items', 'if', 'then', 'else'].includes(key))
        audit(value, [...path, key]);
      if (key === 'allOf' || key === 'oneOf')
        value.forEach((child, i) => audit(child, [...path, key, i]));
    }
    if (
      node.type === 'object' &&
      node.additionalProperties !== false &&
      !(
        path.join('/') === '$defs/CommandPayload' &&
        node.additionalProperties === true &&
        node.maxProperties === 1024 &&
        Object.keys(node).every((key) =>
          ['type', 'additionalProperties', 'maxProperties'].includes(key),
        )
      )
    )
      throw Error('object must be closed');
  }
  audit(schema);
  const title = (s) =>
    s
      .split('_')
      .map((s) => (s === 'id' ? 'ID' : s[0].toUpperCase() + s.slice(1)))
      .join('');
  function type(s, lang) {
    if (s.oneOf) {
      if (lang !== 'ts') throw Error('oneOf requires named Go wrapper');
      return s.oneOf.map((branch) => type(branch, lang)).join(' | ');
    }
    if (s === schema.$defs.CommandPayload)
      return lang === 'go' ? 'json.RawMessage' : 'Record<string, unknown>';
    if (s.const !== undefined && lang === 'ts') return JSON.stringify(s.const);
    if (s.$ref) {
      if (!s.$ref.startsWith('#/$defs/'))
        throw Error('external refs unsupported');
      return s.$ref.slice(8);
    }
    if (Array.isArray(s.type)) {
      if (JSON.stringify(s.type) !== '["string","null"]')
        throw Error('unsupported nullable type');
      return lang === 'go' ? '*string' : 'string | null';
    }
    if (s.enum) {
      if (lang === 'ts') return s.enum.map(JSON.stringify).join(' | ');
    }
    switch (s.type) {
      case 'string':
        return 'string';
      case 'boolean':
        return lang === 'go' ? 'bool' : 'boolean';
      case 'array':
        return lang === 'go'
          ? '[]' + type(s.items, lang)
          : `Array<${type(s.items, lang)}>`;
      case 'object':
        return lang === 'go'
          ? `struct {\n${Object.entries(s.properties)
              .map(
                ([k, v]) =>
                  `${title(k)} ${s.required.includes(k) ? type(v, lang) : '*' + type(v, lang)} \x60json:"${k}${s.required.includes(k) ? '' : ',omitempty'}"\x60`,
              )
              .join('\n')}\n}`
          : `{\n${Object.entries(s.properties)
              .map(
                ([k, v]) =>
                  `${k}${s.required.includes(k) ? '' : '?'}: ${type(v, lang)};`,
              )
              .join('\n')}\n}`;
      default:
        throw Error(`unsupported type ${s.type}`);
    }
  }
  function unionGo(name, definition) {
    const branches = definition.oneOf.map((branch) => {
      if (
        Object.keys(branch).length !== 1 ||
        !branch.$ref?.startsWith('#/$defs/')
      )
        throw Error('union branches must be named local refs');
      const variant = branch.$ref.slice(8);
      const node = schema.$defs[variant];
      if (node?.type !== 'object')
        throw Error('union variants must be objects');
      return { variant, node };
    });
    const discriminants = Object.keys(branches[0].node.properties).filter(
      (key) =>
        branches.every(
          ({ node }) =>
            typeof node.properties[key]?.const === 'string' &&
            node.required.includes(key),
        ),
    );
    const field = discriminants.find(
      (key) =>
        new Set(branches.map(({ node }) => node.properties[key].const)).size ===
        branches.length,
    );
    if (!field)
      throw Error('union requires unique required string discriminant');
    let result = `type ${name} struct { value ${name}Variant }\n`;
    result += `type ${name}Variant interface { is${name}() }\n`;
    for (const { variant, node } of branches) {
      const suffix = variant.slice(name.length);
      result += `func (${variant}) is${name}() {}\n`;
      result += `func New${variant}(value ${variant}) ${name} { value.${title(field)} = ${JSON.stringify(node.properties[field].const)}; return ${name}{value:value} }\n`;
      result += `func (value ${name}) As${suffix}() (${variant}, bool) { variant, ok := value.value.(${variant}); return variant, ok }\n`;
    }
    result += `func (value ${name}) MarshalJSON() ([]byte,error) { if value.value == nil { return nil, fmt.Errorf("empty ${name}") }; return json.Marshal(value.value) }\n`;
    result += `func (value *${name}) UnmarshalJSON(data []byte) error { var tag struct { Tag string \x60json:"${field}"\x60 }; if err := json.Unmarshal(data,&tag); err != nil {return err}; switch tag.Tag {\n`;
    for (const { variant, node } of branches)
      result += `case ${JSON.stringify(node.properties[field].const)}: var branch ${variant}; if err := json.Unmarshal(data,&branch); err != nil {return err}; value.value = branch; return nil\n`;
    result += `}; return fmt.Errorf("unknown ${name} discriminant") }\n`;
    return result;
  }
  let go = `// Code generated by scripts/generate.mjs v1.0.0 from ${source}; DO NOT EDIT.\npackage wire\n\nimport ("encoding/json"; "fmt")\n\n`;
  let ts = `// Code generated by scripts/generate.mjs v1.0.0 from ${source}; DO NOT EDIT.\n`;
  let aliases = `// Code generated by scripts/generate.mjs v1.0.0 from ${source}; DO NOT EDIT.\npackage ${configuration.namespace}\nimport wire "${configuration.goImport}"\n`;
  for (const [name, definition] of Object.entries(schema.$defs)) {
    go += definition.oneOf
      ? unionGo(name, definition)
      : `type ${name} ${type(definition, 'go')}\n`;
    if (definition.oneOf)
      for (const branch of definition.oneOf) {
        const variant = branch.$ref.slice(8);
        aliases += `func New${variant}(value ${variant}) ${name} { return wire.New${variant}(value) }\n`;
      }
    if (name === 'CommandPayload')
      go += `func (value CommandPayload) MarshalJSON() ([]byte, error) { return json.RawMessage(value).MarshalJSON() }
func (value *CommandPayload) UnmarshalJSON(data []byte) error { var raw json.RawMessage; if err := raw.UnmarshalJSON(data); err != nil { return err }; *value = CommandPayload(raw); return nil }
`;
    ts += `export type ${name} = ${type(definition, 'ts')};\n`;
    aliases += `type ${name} = wire.${name}\n`;
  }
  aliases += `type Value interface { ${Object.keys(schema.$defs).join(' | ')} }\n`;
  function assertClosedPayload(node, seen = new Set()) {
    if (node.$ref) {
      const name = node.$ref.slice(8);
      if (seen.has(name)) return;
      seen.add(name);
      const definition = schema.$defs[name];
      if (!definition) throw Error(`unresolved payload ref ${name}`);
      return assertClosedPayload(definition, seen);
    }
    if (node.type === 'object' && node.additionalProperties !== false)
      throw Error('method must have a closed payload schema at every depth');
    for (const child of Object.values(node.properties ?? {}))
      assertClosedPayload(child, seen);
    if (node.items) assertClosedPayload(node.items, seen);
    for (const child of [...(node.allOf ?? []), ...(node.oneOf ?? [])])
      assertClosedPayload(child, seen);
    for (const key of ['if', 'then', 'else'])
      if (node[key]) assertClosedPayload(node[key], seen);
  }
  const inferredMethods = [];
  const methodKeys = new Set();
  for (const [name, def] of Object.entries(schema.$defs)) {
    const properties = def.properties;
    if (!properties) continue;
    const identity = ['contract_version', 'profile', 'method'];
    if (!identity.every((key) => typeof properties[key]?.const === 'string'))
      continue;
    const [version, profile, method] = identity.map(
      (key) => properties[key].const,
    );
    const key = JSON.stringify([version, profile, method]);
    if (methodKeys.has(key))
      throw Error(`duplicate method registration ${key}`);
    const payloadRef = properties.payload?.$ref;
    const payloadDef = schema.$defs[payloadRef?.slice(8)];
    if (
      !payloadRef?.startsWith('#/$defs/') ||
      payloadDef?.type !== 'object' ||
      payloadDef.additionalProperties !== false
    )
      throw Error(`method ${name} must have a closed payload schema`);
    assertClosedPayload(payloadDef);
    methodKeys.add(key);
    inferredMethods.push({ version, profile, method, schema: name });
  }
  // This explicit inventory advertises only methods whose complete path is ready.
  const inventory = JSON.parse(
    readFileSync(
      `contract/schema/${configuration.version}/methods.json`,
      'utf8',
    ),
  );
  if (
    Object.keys(inventory).join() !== 'methods' ||
    !Array.isArray(inventory.methods) ||
    inventory.methods.length === 0
  )
    throw Error('invalid method inventory');
  // Schema metadata numbers are safe integers; wire values never contain numbers.
  function schemaCanonical(value) {
    if (
      typeof value === 'number' &&
      (!Number.isSafeInteger(value) || value < 0 || Object.is(value, -0))
    )
      throw Error('unsafe schema number');
    if (Array.isArray(value))
      return '[' + value.map(schemaCanonical).join(',') + ']';
    if (value !== null && typeof value === 'object')
      return (
        '{' +
        Object.keys(value)
          .sort()
          .map((key) => JSON.stringify(key) + ':' + schemaCanonical(value[key]))
          .join(',') +
        '}'
      );
    return JSON.stringify(value);
  }
  function schemaDigest(root) {
    if (!schema.$defs[root]) throw Error(`unresolved method schema ${root}`);
    const reachable = new Set();
    function visit(node) {
      if (Array.isArray(node)) return node.forEach(visit);
      if (!node || typeof node !== 'object') return;
      if (node.$ref) {
        if (!node.$ref.startsWith('#/$defs/'))
          throw Error('external method ref unsupported');
        const name = node.$ref.slice(8);
        if (!schema.$defs[name]) throw Error(`unresolved method ref ${name}`);
        if (!reachable.has(name)) {
          reachable.add(name);
          visit(schema.$defs[name]);
        }
      }
      Object.values(node).forEach(visit);
    }
    visit({ $ref: '#/$defs/' + root });
    const bundle = {
      $schema: schema.$schema,
      $id: schema.$id,
      $ref: '#/$defs/' + root,
      $defs: Object.fromEntries(
        [...reachable].sort().map((name) => [name, schema.$defs[name]]),
      ),
    };
    return (
      'sha256:' +
      createHash('sha256')
        .update('lerna-schema-digest-1\n' + schemaCanonical(bundle), 'utf8')
        .digest('hex')
    );
  }
  const registeredInputs = new Set();
  const methods = inventory.methods.map((entry) => {
    if (
      Object.keys(entry).sort().join() !==
        (configuration.version === '1.1.0'
          ? 'advertised,input_schema,output_schema'
          : 'input_schema,output_schema') ||
      (configuration.version === '1.1.0' &&
        typeof entry.advertised !== 'boolean') ||
      typeof entry.input_schema !== 'string' ||
      typeof entry.output_schema !== 'string'
    )
      throw Error('invalid method registration');
    if (registeredInputs.has(entry.input_schema))
      throw Error('duplicate method registration');
    registeredInputs.add(entry.input_schema);
    const input = inferredMethods.find(
      (method) => method.schema === entry.input_schema,
    );
    if (!input) throw Error('method input must have constant identity');
    if (
      !['contract_version', 'profile', 'method', 'payload'].every((key) =>
        schema.$defs[entry.input_schema].required?.includes(key),
      )
    )
      throw Error('method identity and payload must be required');
    assertClosedPayload(schema.$defs[entry.output_schema] ?? {});
    return {
      ...input,
      output: entry.output_schema,
      ...(configuration.version === '1.1.0'
        ? { advertised: entry.advertised }
        : {}),
      inputDigest: schemaDigest(entry.input_schema),
      outputDigest: schemaDigest(entry.output_schema),
    };
  });
  const declaredSupport = methods.map((m) => ({
    contract_version: m.version,
    profile: m.profile,
    method: m.method,
    input_schema: m.schema,
    output_schema: m.output,
    input_schema_digest: m.inputDigest,
    output_schema_digest: m.outputDigest,
  }));
  const support = declaredSupport.filter(
    (_, index) => methods[index].advertised ?? true,
  );
  if (configuration.version === '1.1.0') {
    go += `// DeclaredMethods returns the typed inventory; availability is declared separately.\nfunc DeclaredMethods() []MethodSupport { return []MethodSupport{\n`;
    for (const m of declaredSupport)
      go += `{${Object.entries(m)
        .map(([key, value]) => title(key) + ':' + JSON.stringify(value))
        .join(',')}},\n`;
    go += '} }\n';
    ts += `export const declaredMethods = Object.freeze(${JSON.stringify(declaredSupport)}.map(method => Object.freeze(method))) as ReadonlyArray<Readonly<MethodSupport>>;\n`;
  }
  go += `// SupportedMethods returns detached, generated support metadata.\nfunc SupportedMethods() []MethodSupport { return []MethodSupport{\n`;
  for (const m of support)
    go += `{${Object.entries(m)
      .map(([key, value]) => title(key) + ':' + JSON.stringify(value))
      .join(',')}},\n`;
  go += '} }\n';
  ts += `export const supportedMethods = Object.freeze(${JSON.stringify(support)}.map(method => Object.freeze(method))) as ReadonlyArray<Readonly<MethodSupport>>;\n`;
  go += `// InputSchema selects only an explicitly registered method.\nfunc InputSchema(version, profile, method string) (string, bool) {\n`;
  for (const m of methods)
    go += `if version == ${JSON.stringify(m.version)} && profile == ${JSON.stringify(m.profile)} && method == ${JSON.stringify(m.method)} { return ${JSON.stringify(m.schema)}, true }\n`;
  go += `return "", false\n}\n`;
  ts += `export const inputSchemas = Object.freeze((${JSON.stringify(methods)} as const).map(entry => Object.freeze(entry)));\n`;
  go += `const SchemaJSON = ${JSON.stringify(JSON.stringify(schema))}\n`;
  ts += `export interface Values {\n${Object.keys(schema.$defs)
    .map((n) => `${n}: ${n};`)
    .join(
      '\n',
    )}\n}\n// Freeze the public source before any validator can compile it.\nfunction deepFreeze<T>(value: T): T {\n  if (value !== null && typeof value === 'object') {\n    Object.values(value).forEach(deepFreeze);\n    Object.freeze(value);\n  }\n  return value;\n}\nexport const schema = deepFreeze(${JSON.stringify(schema, null, 2)} as const);\n`;
  // The same schema inventory drives the independent Go typed fixture runner.
  const runner = `// Code generated by scripts/generate.mjs v1.0.0 from ${source}; DO NOT EDIT.
package main
import ("fmt"; ${configuration.namespace === 'contract' ? '' : 'contract '}"${configuration.contractImport}")
func run(name string, data []byte) ([]byte, error) {
 if name == "CommandInput" { value, err := contract.DecodeCommand(data); if err != nil { return nil, err }; return contract.Encode(value) }
${
  configuration.version === '1.1.0'
    ? `if name == "DecideInput" { value, err := contract.DecodeDecide(data); if err != nil { return nil, err }; return contract.Encode(value) }
 if name == "GetInput" { value, err := contract.DecodeGet(data); if err != nil { return nil, err }; return contract.Encode(value) }
 if name == "CancelInput" { value, err := contract.DecodeCancel(data); if err != nil { return nil, err }; return contract.Encode(value) }\n`
    : ''
} switch name {
${Object.keys(schema.$defs)
  .map(
    (name) =>
      `case ${JSON.stringify(name)}: return roundtrip[contract.${name}](data)`,
  )
  .join('\n')}
 default: return nil, fmt.Errorf("unknown value schema %q", name)
 }
}
`;
  for (const [path, content] of [
    [configuration.go, go],
    [configuration.runner, runner],
    [configuration.aliases, aliases],
    [configuration.ts, ts],
  ]) {
    const output = path.endsWith('.go')
      ? execFileSync('gofmt', { input: content, encoding: 'utf8' })
      : execFileSync(
          process.execPath,
          [prettierCLI, '--stdin-filepath', path],
          {
            input: content,
            encoding: 'utf8',
          },
        );
    if (process.argv.includes('--check')) {
      if (readFileSync(path, 'utf8') !== output)
        throw Error(`stale generated file ${path}`);
    } else {
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, output);
    }
  }
}
