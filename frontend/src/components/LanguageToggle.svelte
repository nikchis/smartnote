<script lang="ts">
  // LanguageToggle shows the language of the interface, EN or RU, and switches
  // to the other one (spec §16.4: two languages, one button). Go saves the
  // choice first; when it cannot, the language stays and the error is shown.
  import { dialogs } from "../lib/dialogs.svelte";
  import { errorMsg } from "../lib/errors";
  import { i18n, type Msg } from "../lib/i18n.svelte";

  let switching = $state(false);

  async function toggle() {
    if (switching) return;
    switching = true;
    let failure: Msg | undefined;
    try {
      await i18n.set(i18n.lang === "en" ? "ru" : "en");
    } catch (e) {
      failure = errorMsg(e);
    } finally {
      switching = false;
    }
    if (failure) await dialogs.error(failure);
  }
</script>

<button type="button" title={i18n.t("language.label")} aria-label={i18n.t("language.label")} onclick={toggle} disabled={switching}>
  {i18n.t("language.code")}
</button>
