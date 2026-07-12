"use client";

import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

// The @tailwindcss/typography (`prose`) plugin is NOT installed, so element
// styles are supplied explicitly via react-markdown's component map. This keeps
// the note readable (headings, lists, GFM tables for the glossary, links) on our
// own design tokens without adding a dependency. react-markdown does NOT render
// raw HTML by default (no rehype-raw), so the content is safe.
//
// This is the "reader surface": sans body at a comfortable reading size with
// monospace accents for code / arXiv IDs, generous line-height and measure.
const components: Components = {
  h1: (props) => (
    <h1 className="mt-8 mb-4 text-3xl font-semibold tracking-tight text-ink first:mt-0" {...props} />
  ),
  h2: (props) => (
    <h2
      className="mt-8 mb-3 border-b border-line pb-2 text-xl font-semibold text-ink"
      {...props}
    />
  ),
  h3: (props) => <h3 className="mt-6 mb-2 text-lg font-semibold text-ink" {...props} />,
  p: (props) => <p className="my-4 leading-[1.75]" {...props} />,
  ul: (props) => <ul className="my-4 list-disc space-y-1.5 pl-6 marker:text-muted" {...props} />,
  ol: (props) => <ol className="my-4 list-decimal space-y-1.5 pl-6 marker:text-muted" {...props} />,
  li: (props) => <li className="leading-[1.7] pl-1" {...props} />,
  blockquote: (props) => (
    <blockquote className="my-4 border-l-2 border-accent/50 pl-4 text-muted italic" {...props} />
  ),
  a: (props) => (
    <a
      className="font-medium text-accent underline decoration-accent/40 underline-offset-2 hover:decoration-accent"
      target="_blank"
      rel="noreferrer"
      {...props}
    />
  ),
  strong: (props) => <strong className="font-semibold text-ink" {...props} />,
  hr: (props) => <hr className="my-8 border-line" {...props} />,
  table: (props) => (
    <div className="my-5 overflow-x-auto rounded-lg border border-line">
      <table className="w-full border-collapse text-sm" {...props} />
    </div>
  ),
  th: (props) => (
    <th className="border-b border-line bg-card px-3 py-2 text-left font-semibold text-ink" {...props} />
  ),
  td: (props) => (
    <td className="border-b border-line px-3 py-2 text-ink/85 last:border-0" {...props} />
  ),
  code: (props) => (
    <code
      className="rounded border border-line bg-card px-1.5 py-0.5 font-mono text-[0.85em] text-ink"
      {...props}
    />
  ),
  pre: (props) => (
    // Reset the inline-code box on nested <code> so fenced blocks aren't double-boxed.
    <pre
      className="my-4 overflow-x-auto rounded-lg border border-line bg-card p-4 font-mono text-sm [&_code]:border-0 [&_code]:bg-transparent [&_code]:p-0"
      {...props}
    />
  ),
};

export function MarkdownPreview({ content }: { content: string }) {
  return (
    <div className="max-w-[68ch] text-[17px] text-ink">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
