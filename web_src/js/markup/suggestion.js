// This module moves each rendered diff into the place of its
// matching code block, so no diff/escaping/highlighting logic lives on the client.
//
// Matching is positional: the Nth `.suggestion-diff` (data-suggestion-index=N) of
// a comment replaces the Nth suggestion code block of that comment's body. A
// suggestion block with no matching diff (previous side, outdated, plain comment)
// is left untouched and renders as ordinary code.

const suggestionClassRe = /(?:^|\s)language-suggestion(?:\s|$)/i;

function suggestionCodeBlocks(contentEl) {
  const blocks = [];
  for (const code of contentEl.querySelectorAll('pre.code-block > code')) {
    if (suggestionClassRe.test(code.className)) {
      blocks.push(code.parentElement);
    }
  }
  return blocks;
}

export function renderSuggestions(container = document) {
  // Snapshot each comment's suggestion code blocks once: pre.replaceWith() below removes the matched
  // <pre> from the live DOM, so re-querying per holder would shift indices and misplace the Nth block.
  const blocksByContent = new Map();
  for (const holder of container.querySelectorAll('.suggestion-diff[data-comment-id]:not([data-render-done])')) {
    const commentId = holder.getAttribute('data-comment-id');
    const index = Number(holder.getAttribute('data-suggestion-index'));
    // getElementById takes a literal id (no CSS escaping), correct for "issuecomment-<n>-content".
    const content = document.getElementById(`issuecomment-${commentId}-content`);
    if (!content) continue;

    let blocks = blocksByContent.get(content);
    if (!blocks) {
      blocks = suggestionCodeBlocks(content);
      blocksByContent.set(content, blocks);
    }

    const pre = blocks[index];
    if (!pre) continue;

    pre.replaceWith(holder);
    holder.classList.remove('tw-hidden');
    // Mark done only after a successful swap, so an early call (before the code block
    // is in the DOM) can still render it on a later pass.
    holder.setAttribute('data-render-done', 'true');
  }
}
