"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { fetchCategories } from "@/lib/api";

// CategoryPicker lets the user choose which arXiv cs.* category to explore and
// optionally narrow it with free-text keywords. It is a controlled component:
// the parent owns category/terms state so the trigger action reads the current
// selection directly. Category is required; terms is optional.
//
// The category list AND the initial default both come from the backend, so the
// picker's default matches the same configured default the empty-body discovery
// path would use (no frontend/backend divergence). The parent starts `category`
// empty; this component seeds it once the catalog loads.
export function CategoryPicker({
  category,
  terms,
  onCategoryChange,
  onTermsChange,
  disabled,
}: {
  category: string;
  terms: string;
  onCategoryChange: (code: string) => void;
  onTermsChange: (terms: string) => void;
  disabled?: boolean;
}) {
  // Catalog rarely changes within a session, so cache it indefinitely.
  const { data } = useQuery({
    queryKey: ["categories"],
    queryFn: fetchCategories,
    staleTime: Infinity,
  });

  // Seed the selection from the backend default once, only if the parent has not
  // set a category yet — never clobber a user choice on a later refetch.
  useEffect(() => {
    if (data?.default && !category) {
      onCategoryChange(data.default);
    }
  }, [data?.default, category, onCategoryChange]);

  return (
    <div className="flex flex-wrap items-end gap-3">
      <label className="flex flex-col gap-1 text-xs text-muted">
        Category
        <select
          value={category}
          disabled={disabled || !data}
          onChange={(e) => onCategoryChange(e.target.value)}
          className="min-w-56 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-accent focus:outline-none disabled:opacity-60"
        >
          {/* Placeholder until the catalog loads; the effect above then seeds a
              real value, so this option is transient. */}
          {!data && <option value="">Loading…</option>}
          {data?.categories.map((c) => (
            <option key={c.code} value={c.code}>
              {c.label} ({c.code})
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1 text-xs text-muted">
        Keywords (optional)
        <input
          type="text"
          value={terms}
          disabled={disabled}
          placeholder="e.g. diffusion models"
          onChange={(e) => onTermsChange(e.target.value)}
          className="min-w-56 rounded-lg border border-line bg-surface px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none disabled:opacity-60"
        />
      </label>
    </div>
  );
}
