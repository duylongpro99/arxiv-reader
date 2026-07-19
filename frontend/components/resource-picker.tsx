"use client";

import type { ResourceDescriptor } from "@/lib/types";

// ResourcePicker lets the user choose which resource to explore. It is hidden
// when only one resource exists (the common v1 case: arXiv), so the UI stays
// uncluttered until a second resource is added. Controlled: the parent owns the
// selected id and resets the form values to the new resource's defaults on change.
export function ResourcePicker({
  resources,
  resourceId,
  onChange,
  disabled,
}: {
  resources: ResourceDescriptor[];
  resourceId: string;
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  if (resources.length <= 1) {
    return null; // nothing to pick between
  }
  const current = resources.find((r) => r.id === resourceId);
  return (
    <label className="flex flex-col gap-1 text-xs text-muted">
      Resource
      <select
        value={resourceId}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="min-w-56 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-accent focus:outline-none disabled:opacity-60"
      >
        {resources.map((r) => (
          <option key={r.id} value={r.id}>
            {r.label}
          </option>
        ))}
      </select>
      {current?.description && (
        <span className="text-xs text-muted">{current.description}</span>
      )}
    </label>
  );
}
