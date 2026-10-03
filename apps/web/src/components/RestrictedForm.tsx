import { useEffect, useId, useState } from "react";
import { CORE_SCHEMA, isObject, parseStrict } from "@harness/sdk";
import type { JSONValue, Schema } from "@harness/sdk";
const labels: Record<string, string> = {
  reason: "理由",
  task_id: "原 Task ID",
  memory_id: "原 Memory ID",
  session_id: "原 Session ID",
  branch_id: "原分支 ID",
  target_task_ref: "原 Task 准确引用",
  expected_goal_revision: "原目标修订",
  expected_branch_revision: "原分支修订",
  content_ref: "准确正文引用",
  goal_ref: "准确目标正文引用",
  policy_ref: "固定策略引用",
  deadline: "领域截止（UTC）",
  budget: "分单位预算",
  values: "准确记忆值",
  limit: "每页数量",
  cursor: "原分页游标",
  request_ref: "准确输入请求版本",
  answer_ref: "准确回答正文引用",
  preview_refs: "预览关联引用",
  confirmation_id: "原确认 ID",
  decision: "本人决定",
  prepare_deadline: "准备完成截止（UTC）",
  title: "报告标题",
  body: "报告正文",
  save_path: "输出文件",
};
export function resolveSchema(value: unknown, depth = 0): Schema {
  if (depth > 8 || !isObject(value)) throw new Error("不支持此表单 Schema");
  if (typeof value.$ref === "string") {
    if (!value.$ref.startsWith("#/$defs/")) throw new Error("不允许远端 Schema");
    const name = value.$ref.slice("#/$defs/".length);
    const definitions = CORE_SCHEMA.$defs as unknown as Record<string, unknown>;
    return resolveSchema(definitions[name], depth + 1);
  }
  return value;
}
export function initialPayload(schema: unknown, depth = 0): JSONValue {
  const spec = resolveSchema(schema, depth);
  if (Array.isArray(spec.oneOf) && spec.oneOf.length > 0 && spec.oneOf.length <= 8)
    return initialPayload(spec.oneOf[0], depth + 1);
  if (spec.const !== undefined) return spec.const;
  if (Array.isArray(spec.enum) && spec.enum.length) return spec.enum[0] ?? "";
  if (spec.type === "object") {
    if (spec.additionalProperties !== false || !isObject(spec.properties))
      throw new Error("表单只接受闭合对象");
    const result: Record<string, JSONValue> = {};
    const required = Array.isArray(spec.required) ? spec.required : [];
    for (const key of required)
      if (typeof key === "string")
        result[key] = key === "limit" ? 20 : initialPayload(spec.properties[key], depth + 1);
    return result;
  }
  if (spec.type === "array") return [];
  if (spec.type === "boolean") return false;
  if (spec.type === "integer" || spec.type === "number")
    return typeof spec.minimum === "number" ? spec.minimum : 0;
  if (spec.type === "string") return "";
  throw new Error("此字段不是受限表单支持的类型");
}
function JSONField({
  name,
  value,
  onChange,
  onValidity,
}: {
  name: string;
  value: JSONValue;
  onChange: (value: JSONValue) => void;
  onValidity: (valid: boolean) => void;
}) {
  const [text, setText] = useState(() => JSON.stringify(value, null, 2));
  const [error, setError] = useState("");
  useEffect(() => {
    setText(JSON.stringify(value, null, 2));
    setError("");
  }, [value]);
  return (
    <>
      <textarea
        className="json-field"
        aria-label={name}
        value={text}
        rows={Math.min(8, Math.max(3, text.split("\n").length))}
        maxLength={262144}
        spellCheck={false}
        onChange={(event) => {
          const next = event.target.value;
          setText(next);
          try {
            const parsed = parseStrict(next);
            setError("");
            onValidity(true);
            onChange(parsed);
          } catch {
            setError("必须是完整且准确的 JSON，不接受重复键或非法数值");
            onValidity(false);
          }
        }}
      />
      {error && (
        <span className="field-error" role="alert">
          {error}
        </span>
      )}
    </>
  );
}
export function RestrictedForm({
  schema,
  value,
  onChange,
  onValidity,
}: {
  schema: Schema;
  value: JSONValue;
  onChange: (value: JSONValue) => void;
  onValidity?: (valid: boolean) => void;
}) {
  const prefix = useId();
  const [invalid, setInvalid] = useState<Record<string, boolean>>({});
  let spec: Schema;
  try {
    spec = resolveSchema(schema);
  } catch (error) {
    return <p className="notice">{error instanceof Error ? error.message : "不支持此表单"}</p>;
  }
  if (Array.isArray(spec.oneOf)) {
    if (!spec.oneOf.length || spec.oneOf.length > 8)
      return <p className="notice">此表单的格式分支超过受信界面的上限。</p>;
    return (
      <UnionForm
        schema={spec}
        value={value}
        onChange={onChange}
        {...(onValidity ? { onValidity } : {})}
      />
    );
  }
  if (
    spec.type !== "object" ||
    spec.additionalProperties !== false ||
    !isObject(spec.properties) ||
    Object.keys(spec.properties).length > 50 ||
    !isObject(value)
  )
    return (
      <p className="notice">此方法未提供可呈现的闭合受限表单。必须使用原 owner 的准确合同。</p>
    );
  const properties = spec.properties;
  const required = Array.isArray(spec.required) ? spec.required : [];
  return (
    <div className="restricted-form">
      {Object.entries(properties).map(([key, raw]) => {
        let field: Schema;
        try {
          field = resolveSchema(raw);
        } catch {
          return (
            <p key={key} className="notice">
              {key} 的字段类型尚不支持
            </p>
          );
        }
        const included = Object.hasOwn(value, key);
        const obligatory = required.includes(key);
        const label = labels[key] ?? key;
        const update = (next: JSONValue) => onChange({ ...value, [key]: next });
        const validity = (valid: boolean) => {
          const next = { ...invalid, [key]: !valid };
          setInvalid(next);
          onValidity?.(!Object.values(next).some(Boolean));
        };
        const title = `${prefix}-${key}`;
        const fieldValue = value[key] ?? "";
        const remove = () => {
          const next = { ...value };
          delete next[key];
          onChange(next);
          validity(true);
        };
        return (
          <div className="field" key={key}>
            <div className="field-title">
              <label htmlFor={title}>
                {label}
                {obligatory ? "" : "（可选）"}
              </label>
              {!obligatory && (
                <button
                  className="text-button"
                  type="button"
                  onClick={() => {
                    if (included) remove();
                    else update(initialPayload(field));
                  }}
                >
                  {included ? "移除此字段" : "填写此字段"}
                </button>
              )}
            </div>
            {(included || obligatory) &&
              (field.const !== undefined ? (
                <input id={title} aria-label={label} readOnly value={String(field.const)} />
              ) : Array.isArray(field.enum) ? (
                <select
                  id={title}
                  aria-label={label}
                  value={String(fieldValue)}
                  onChange={(event) => {
                    const selected = Array.isArray(field.enum)
                      ? field.enum.find((option) => String(option) === event.target.value)
                      : undefined;
                    if (selected !== undefined) update(selected);
                  }}
                >
                  {field.enum.map((option) => (
                    <option key={String(option)} value={String(option)}>
                      {String(option)}
                    </option>
                  ))}
                </select>
              ) : field.type === "boolean" ? (
                <label className="check-label">
                  <input
                    id={title}
                    type="checkbox"
                    checked={fieldValue === true}
                    onChange={(event) => update(event.target.checked)}
                  />
                  启用该声明
                </label>
              ) : field.type === "string" &&
                (key === "body" ||
                  (typeof field.maxLength === "number" && field.maxLength > 4096)) ? (
                <textarea
                  id={title}
                  aria-label={label}
                  value={typeof fieldValue === "string" ? fieldValue : ""}
                  rows={6}
                  maxLength={
                    typeof field.maxLength === "number" ? Math.min(field.maxLength, 65536) : 4096
                  }
                  onChange={(event) => update(event.target.value)}
                />
              ) : field.type === "string" ? (
                <input
                  id={title}
                  aria-label={label}
                  value={typeof fieldValue === "string" ? fieldValue : ""}
                  maxLength={
                    typeof field.maxLength === "number" ? Math.min(field.maxLength, 4096) : 4096
                  }
                  onChange={(event) => update(event.target.value)}
                />
              ) : field.type === "integer" || field.type === "number" ? (
                <input
                  id={title}
                  aria-label={label}
                  type="number"
                  value={typeof fieldValue === "number" ? fieldValue : ""}
                  min={typeof field.minimum === "number" ? field.minimum : undefined}
                  max={typeof field.maximum === "number" ? field.maximum : undefined}
                  step={field.type === "integer" ? 1 : "any"}
                  onChange={(event) => {
                    const number = Number(event.target.value);
                    const valid =
                      event.target.value !== "" &&
                      Number.isFinite(number) &&
                      Math.abs(number) <= Number.MAX_SAFE_INTEGER;
                    validity(valid);
                    if (valid) update(number);
                  }}
                />
              ) : field.type === "object" || field.type === "array" ? (
                <JSONField
                  name={label}
                  value={value[key] ?? initialPayload(field)}
                  onChange={update}
                  onValidity={validity}
                />
              ) : (
                <p className="notice">该字段类型未开放</p>
              ))}
            {typeof field.description === "string" && (
              <span className="field-hint">{field.description}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}
function UnionForm({
  schema,
  value,
  onChange,
  onValidity,
}: {
  schema: Schema;
  value: JSONValue;
  onChange: (value: JSONValue) => void;
  onValidity?: (valid: boolean) => void;
}) {
  const [selected, setSelected] = useState(0);
  const id = useId();
  const branches = Array.isArray(schema.oneOf) ? schema.oneOf : [];
  let formats: Array<{ spec: Schema; kind: JSONValue | undefined; label: string }>;
  try {
    formats = branches.map((branch, index) => {
      const spec = resolveSchema(branch);
      const kind =
        isObject(spec.properties) && isObject(spec.properties.kind)
          ? spec.properties.kind.const
          : undefined;
      return { spec, kind, label: typeof kind === "string" ? kind : `格式 ${index + 1}` };
    });
  } catch {
    return <p className="notice">此格式包含尚未支持的本地 Schema 引用，依赖提交保持关闭。</p>;
  }
  const bound = isObject(value)
    ? formats.findIndex((format) => format.kind !== undefined && format.kind === value.kind)
    : -1;
  const index = bound >= 0 ? bound : selected;
  const current = formats[index];
  if (
    !current ||
    formats.some(
      (format) => format.spec.type !== "object" || format.spec.additionalProperties !== false,
    )
  )
    return <p className="notice">此格式分支未提供闭合受限对象。</p>;
  return (
    <>
      <div className="field">
        <label htmlFor={id}>回答格式</label>
        <select
          id={id}
          value={index}
          onChange={(event) => {
            const next = Number(event.target.value);
            const format = formats[next];
            if (!format) return;
            setSelected(next);
            onChange(initialPayload(format.spec));
            onValidity?.(true);
          }}
        >
          {formats.map((format, position) => (
            <option key={format.label} value={position}>
              {format.label}
            </option>
          ))}
        </select>
      </div>
      <RestrictedForm
        key={index}
        schema={current.spec}
        value={value}
        onChange={onChange}
        {...(onValidity ? { onValidity } : {})}
      />
    </>
  );
}
