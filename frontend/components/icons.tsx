// Local inline SVG icons (stroke 1.5, currentColor). Kept in-repo instead of
// pulling a dependency for ~10 glyphs — leaner and theme-driven via currentColor.
// All are aria-hidden by default; wrap in a labelled control for meaning.

type IconProps = { className?: string };

function svg(path: React.ReactNode, extra?: { fill?: boolean }) {
  return function Icon({ className }: IconProps) {
    return (
      <svg
        className={className}
        viewBox="0 0 24 24"
        fill={extra?.fill ? "currentColor" : "none"}
        stroke={extra?.fill ? "none" : "currentColor"}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {path}
      </svg>
    );
  };
}

export const SparkIcon = svg(
  <>
    <path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9L12 3z" />
    <path d="M19 3.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7.7-1.8z" />
  </>,
);

export const ArrowRightIcon = svg(<><path d="M5 12h14" /><path d="M13 6l6 6-6 6" /></>);
export const ArrowLeftIcon = svg(<><path d="M19 12H5" /><path d="M11 6l-6 6 6 6" /></>);
export const ChevronRightIcon = svg(<path d="M9 6l6 6-6 6" />);
export const ChevronDownIcon = svg(<path d="M6 9l6 6 6-6" />);
export const CheckIcon = svg(<path d="M20 6L9 17l-5-5" />);
export const CheckCircleIcon = svg(<><circle cx="12" cy="12" r="9" /><path d="M8.5 12.5l2.5 2.5 4.5-5" /></>);
export const XIcon = svg(<><path d="M18 6L6 18" /><path d="M6 6l12 12" /></>);
export const AlertTriangleIcon = svg(
  <>
    <path d="M10.3 3.9L1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
    <path d="M12 9v4" />
    <path d="M12 17h.01" />
  </>,
);
export const DotIcon = svg(<circle cx="12" cy="12" r="4" />, { fill: true });
export const ClockIcon = svg(<><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>);
export const FileTextIcon = svg(
  <>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
    <path d="M14 3v5h5" />
    <path d="M9 13h6" />
    <path d="M9 17h6" />
  </>,
);
export const ExternalLinkIcon = svg(
  <>
    <path d="M15 3h6v6" />
    <path d="M10 14L21 3" />
    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
  </>,
);

// Animated loader — pairs with `animate-spin`; aria-hidden, feedback only.
export function SpinnerIcon({ className }: IconProps) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      aria-hidden="true"
    >
      <path d="M21 12a9 9 0 1 1-9-9" opacity={0.9} />
      <path d="M21 12a9 9 0 0 0-9-9" opacity={0.25} />
    </svg>
  );
}
