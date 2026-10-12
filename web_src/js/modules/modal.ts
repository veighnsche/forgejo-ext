// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// The currently wired approve/cancel handlers per modal element. Re-showing
// a modal replaces them instead of stacking another action on its buttons.
const modalHandlers = new WeakMap<HTMLElement, {ok?: () => void; cancel: () => void}>();

// showModal will show the given modal and run `onApprove` if the approve/ok/yes
// button is pressed.
export function showModal(modalID: string, onApprove: () => void) {
  const modal = document.getElementById(modalID) as HTMLDialogElement;
  // Move the modal to `<body>`, to avoid inheriting any bad CSS or if the
  // parent becomes `display: hidden`.
  document.body.append(modal);

  const okButton = modal.querySelector('.ok');
  const cancelButton = modal.querySelector('.cancel');
  const previous = modalHandlers.get(modal);
  if (previous?.ok) okButton?.removeEventListener('click', previous.ok);
  if (previous) cancelButton?.removeEventListener('click', previous.cancel);
  const handlers: {ok?: () => void; cancel: () => void} = {
    cancel: () => {
      modal.close();
    },
  };
  if (onApprove) handlers.ok = onApprove;
  modalHandlers.set(modal, handlers);

  // Close the modal if the cancel button is pressed.
  cancelButton?.addEventListener('click', handlers.cancel, {passive: true});
  if (handlers.ok) okButton?.addEventListener('click', handlers.ok, {passive: true});

  // The modal is ready to be shown.
  modal.showModal();
}

let modalCloseBound = false;

// NOTE: Can be replaced in late 2026 with `closedBy` attribute on `<dialog>` element.
export function initModalClose() {
  if (modalCloseBound) return;
  modalCloseBound = true;
  document.addEventListener('click', (event) => {
    const dialog = document.querySelector<HTMLDialogElement>('dialog[open]');
    // No open dialogs on page, nothing to do.
    if (dialog === null) return;

    const target = event.target as HTMLElement;
    // User clicked dialog itself (not it's content), likely ::backdrop, so close it.
    if (dialog === target) dialog.close();
  });
}
