export const maxBodyBytes = 1_048_576;
export const maxDepth = 64;

// Reject JavaScript's isolated UTF-16 surrogates before TextEncoder can replace them.
export function assertUnicode(text: string): void {
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const low = text.charCodeAt(++i);
      if (!(low >= 0xdc00 && low <= 0xdfff))
        throw new Error('isolated high surrogate');
    } else if (unit >= 0xdc00 && unit <= 0xdfff)
      throw new Error('isolated low surrogate');
  }
}

// Parse raw bytes before information is lost by JSON.parse. Numbers are excluded
// from version 1 wire values: use the schema's exact decimal strings instead.
export function parseJSON(input: string | Uint8Array): unknown {
  let wire: string;
  if (typeof input === 'string') {
    assertUnicode(input);
    if (new TextEncoder().encode(input).length > maxBodyBytes)
      throw new Error('body exceeds byte limit');
    wire = input;
  } else {
    if (input.length > maxBodyBytes) throw new Error('body exceeds byte limit');
    wire = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(
      input,
    );
  }
  let position = 0;
  const whitespace = () => {
    while (' \t\r\n'.includes(wire[position] ?? '\u0000')) position++;
  };
  const string = (): string => {
    const start = position++;
    while (position < wire.length) {
      const char = wire[position++];
      if (char === '\\') {
        position++;
        continue;
      }
      if (char === '"') {
        const value: unknown = JSON.parse(wire.slice(start, position));
        if (typeof value !== 'string') throw new Error('invalid string');
        assertUnicode(value);
        return value;
      }
    }
    throw new Error('unterminated string');
  };
  const value = (depth: number): unknown => {
    whitespace();
    const char = wire[position];
    if (char === '"') return string();
    for (const [token, parsed] of [
      ['true', true],
      ['false', false],
      ['null', null],
    ] as const) {
      if (wire.startsWith(token, position)) {
        position += token.length;
        return parsed;
      }
    }
    if (char !== '{' && char !== '[')
      throw new Error('invalid JSON token or unsupported number');
    if (++depth > maxDepth) throw new Error('container depth exceeds limit');
    position++;
    whitespace();
    if (char === '[') {
      const result: unknown[] = [];
      if (wire[position] === ']') {
        position++;
        return result;
      }
      for (;;) {
        result.push(value(depth));
        whitespace();
        const separator = wire[position++];
        if (separator === ']') return result;
        if (separator !== ',') throw new Error('invalid array separator');
      }
    }
    const result: Record<string, unknown> = Object.create(null);
    if (wire[position] === '}') {
      position++;
      return result;
    }
    for (;;) {
      whitespace();
      if (wire[position] !== '"') throw new Error('invalid object key');
      const key = string();
      whitespace();
      if (Object.hasOwn(result, key)) throw new Error(`duplicate key ${key}`);
      if (wire[position++] !== ':') throw new Error('missing colon');
      result[key] = value(depth);
      whitespace();
      const separator = wire[position++];
      if (separator === '}') return result;
      if (separator !== ',') throw new Error('invalid object separator');
    }
  };
  const result = value(0);
  whitespace();
  if (position !== wire.length) throw new Error('trailing JSON data');
  return result;
}
