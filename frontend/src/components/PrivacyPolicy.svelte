<script lang="ts">
  // PrivacyPolicy is the privacy policy of the site as a modal window, in the
  // language of the interface. The texts are copies of the site pages
  // (docs/development.md says how to keep them in step); links leave the
  // application through the system browser.
  import { openExternal } from "../lib/external";
  import { i18n } from "../lib/i18n.svelte";
  import { modal } from "../lib/modal";
  import en from "../lib/privacy/en.html?raw";
  import ru from "../lib/privacy/ru.html?raw";

  let { onClose }: { onClose: () => void } = $props();

  const policies = { en, ru };

  function onkeydown(e: KeyboardEvent) {
    if (e.key !== "Escape") return;
    e.preventDefault();
    onClose();
  }

  /** browserLinks: the policy is static text of the repository; a click on a link opens it in the browser. */
  function browserLinks(node: HTMLElement) {
    const onclick = (e: MouseEvent) => {
      if (!(e.target instanceof Element)) return;
      const href = e.target.closest("a")?.getAttribute("href");
      if (!href) return;
      e.preventDefault();
      void openExternal(href).catch(() => {});
    };
    node.addEventListener("click", onclick);
    return { destroy: () => node.removeEventListener("click", onclick) };
  }
</script>

<svelte:window {onkeydown} />

<div class="backdrop" use:modal>
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="privacy-title">
    <h2 id="privacy-title">{i18n.t("privacy.title")}</h2>
    <div class="policy" use:browserLinks>
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      {@html policies[i18n.lang]}
    </div>
    <div class="buttons">
      <button type="button" onclick={onClose}>{i18n.t("common.close")}</button>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: grid;
    place-items: center;
    background: var(--backdrop);
  }
  .dialog {
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    width: min(42rem, calc(100vw - 2rem));
    max-height: calc(100vh - 2rem);
    padding: 1.5rem 2rem 1.25rem;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--surface);
    box-shadow: 0 12px 32px rgb(0 0 0 / 0.28);
  }
  h2 {
    margin: 0 0 0.75rem;
    font-size: 1.2rem;
    font-weight: 700;
    text-align: center;
  }
  .policy {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding-right: 0.5rem;
    line-height: 1.45;
  }
  .policy :global(h2) {
    margin: 1.1rem 0 0.4rem;
    font-size: 1.05rem;
    text-align: left;
  }
  .policy :global(h3) {
    margin: 0.9rem 0 0.3rem;
    font-size: 0.95rem;
  }
  .policy :global(p),
  .policy :global(ul) {
    margin: 0.4rem 0;
  }
  .policy :global(.note) {
    padding: 0.1rem 0.75rem;
    border-left: 3px solid var(--accent);
    background: var(--row);
  }
  .policy :global(code) {
    overflow-wrap: anywhere;
  }
  .buttons {
    display: flex;
    justify-content: center;
    margin-top: 1rem;
    padding-top: 1rem;
    border-top: 1px solid var(--border);
  }
</style>
