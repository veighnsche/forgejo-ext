export async function mount(root, {apiBase}) {
  const description = document.createElement('p');
  description.textContent = 'This page and its backend were installed independently of the Forgejo executable.';
  const output = document.createElement('pre');
  output.dataset.extensionContext = '';
  output.textContent = 'Loading native request context…';
  root.append(description, output);
  const controller = new AbortController();
  try {
    // eslint-disable-next-line no-restricted-syntax -- This standalone package cannot import Forgejo's bundled fetch module.
    const response = await fetch(`${apiBase}context`, {signal: controller.signal});
    if (!response.ok) throw new Error(`Request failed: ${response.status}`);
    output.textContent = JSON.stringify(await response.json(), null, 2);
  } catch (error) {
    if (error.name !== 'AbortError') output.textContent = error.message;
  }
  return () => controller.abort();
}
