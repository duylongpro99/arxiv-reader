"use client";

import type { ResourceField } from "@/lib/types";

// DynamicRequestForm renders a resource's declared fields by type — the
// self-describing UI. No field is hardcoded: a select becomes a dropdown from its
// options, a text field becomes a text input. Adding a resource with new fields
// needs ZERO changes here. Controlled: the parent owns the values map and gets
// per-field changes via onChange(name, value).
export function DynamicRequestForm({
  fields,
  values,
  onChange,
  disabled,
}: {
  fields: ResourceField[];
  values: Record<string, string>;
  onChange: (name: string, value: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-wrap items-end gap-3">
      {fields.map((f) => (
        <label key={f.name} className="flex flex-col gap-1 text-xs text-muted">
          {f.label}
          {f.type === "select" ? (
            <select
              value={values[f.name] ?? ""}
              disabled={disabled}
              onChange={(e) => onChange(f.name, e.target.value)}
              className="min-w-56 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-accent focus:outline-none disabled:opacity-60"
            >
              {(f.options ?? []).map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label} ({o.value})
                </option>
              ))}
            </select>
          ) : (
            <input
              type="text"
              value={values[f.name] ?? ""}
              disabled={disabled}
              placeholder={f.label}
              onChange={(e) => onChange(f.name, e.target.value)}
              className="min-w-56 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none disabled:opacity-60"
            />
          )}
        </label>
      ))}
    </div>
  );
}
