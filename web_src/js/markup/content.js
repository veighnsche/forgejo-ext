import {renderMermaid, retireMermaid} from './mermaid.js';
import {renderMath} from './math.js';
import {renderCodeCopy} from './codecopy.js';
import {renderAsciicast, retireAsciicast} from './asciicast.js';
import {renderExternal, retireExternal} from './external.js';
import {initMarkupTasklist, retireMarkupTasklist} from './tasklist.js';

export function retireMarkupContent() {
  retireMermaid();
  retireAsciicast();
  retireExternal();
}

export function retireCommentContent() {
  retireMarkupTasklist();
}

// code that runs for all markup content
export function initMarkupContent() {
  renderMermaid();
  renderMath();
  renderCodeCopy();
  renderAsciicast();
  renderExternal();
}

// code that only runs for comments
export function initCommentContent() {
  initMarkupTasklist();
}
