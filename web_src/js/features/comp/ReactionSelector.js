import $ from 'jquery';
import {POST} from '../../modules/fetch.js';
import {contributionRootSelector} from '../../native-lifecycle.js';

const attached = new WeakSet();

export function initCompReactionSelector($parent, {signal} = {}) {
  for (const element of $parent.find('.select-reaction .item.reaction, .comment-reaction-button')) {
    if (attached.has(element) || element.closest(contributionRootSelector)) continue;
    attached.add(element);
    signal?.addEventListener('abort', () => attached.delete(element), {once: true});
    element.addEventListener('click', async function (e) {
      e.preventDefault();

      if (this.classList.contains('disabled')) return;

      const actionUrl = this.closest('[data-action-url]')?.getAttribute('data-action-url');
      const reactionContent = this.getAttribute('data-reaction-content');
      const hasReacted = this.closest('.comment')?.querySelector(`.ui.segment.reactions a[data-reaction-content="${reactionContent}"]`)?.getAttribute('data-has-reacted') === 'true';

      const res = await POST(`${actionUrl}/${hasReacted ? 'unreact' : 'react'}`, {
        signal,
        data: new URLSearchParams({content: reactionContent}),
      }).catch((error) => {
        if (!signal?.aborted) throw error;
      });
      if (signal?.aborted || !this.isConnected) return;

      const data = await res.json();
      if (signal?.aborted || !this.isConnected) return;
      if (data && (data.html || data.empty)) {
        const $content = $(this).closest('.content');
        let $react = $content.find('.segment.reactions');
        if ((!data.empty || data.html === '') && $react.length > 0) {
          $react.remove();
        }
        if (!data.empty) {
          const $attachments = $content.find('.segment.bottom').first();
          $react = $(data.html);
          if ($attachments.length > 0) {
            $react.insertBefore($attachments);
          } else {
            $react.appendTo($content);
          }
          $react.find('.dropdown').dropdown();
          initCompReactionSelector($react, {signal});
        }
      }
    }, {signal});
  }
}
