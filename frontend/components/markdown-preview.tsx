"use client";

import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

// The @tailwindcss/typography (`prose`) plugin is NOT installed, so element
// styles are supplied explicitly via react-markdown's component map. This keeps
// the note readable (headings, lists, GFM tables for the glossary, links)
// without adding a dependency just for typography. react-markdown does NOT
// render raw HTML by default (no rehype-raw), so the content is safe.
const components: Components = {
  h1: (props) => <h1 className="mt-6 mb-3 text-2xl font-bold" {...props} />,
  h2: (props) => (
    <h2
      className="mt-6 mb-2 border-b border-gray-200 pb-1 text-xl font-semibold dark:border-gray-700"
      {...props}
    />
  ),
  h3: (props) => <h3 className="mt-4 mb-2 text-lg font-semibold" {...props} />,
  p: (props) => <p className="my-3 leading-relaxed" {...props} />,
  ul: (props) => <ul className="my-3 list-disc pl-6" {...props} />,
  ol: (props) => <ol className="my-3 list-decimal pl-6" {...props} />,
  li: (props) => <li className="my-1" {...props} />,
  a: (props) => (
    <a
      className="text-blue-600 underline hover:text-blue-800 dark:text-blue-400"
      target="_blank"
      rel="noreferrer"
      {...props}
    />
  ),
  strong: (props) => <strong className="font-semibold" {...props} />,
  table: (props) => (
    <div className="my-4 overflow-x-auto">
      <table className="w-full border-collapse text-sm" {...props} />
    </div>
  ),
  th: (props) => (
    <th
      className="border border-gray-300 bg-gray-100 px-3 py-2 text-left font-semibold dark:border-gray-700 dark:bg-gray-800"
      {...props}
    />
  ),
  td: (props) => (
    <td className="border border-gray-300 px-3 py-2 dark:border-gray-700" {...props} />
  ),
  code: (props) => (
    <code
      className="rounded bg-gray-100 px-1 py-0.5 font-mono text-sm dark:bg-gray-800"
      {...props}
    />
  ),
};

export function MarkdownPreview({ content }: { content: string }) {
  return (
    <div className="text-sm text-gray-800 dark:text-gray-200">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
