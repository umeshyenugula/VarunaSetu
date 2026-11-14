import { LOGO_PATH, LOGO_VIEWBOX } from './logoPath';

/** The VarunaSetu wordmark as inline vector. It paints with currentColor, so it follows the surrounding ink. */
export default function Wordmark({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox={LOGO_VIEWBOX} role="img" aria-label="VarunaSetu" xmlns="http://www.w3.org/2000/svg">
      <path fill="currentColor" fillRule="evenodd" d={LOGO_PATH} />
    </svg>
  );
}
