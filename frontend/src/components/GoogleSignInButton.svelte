<script lang="ts">
  // GoogleSignInButton is the "Sign in with Google" button of the dark theme
  // of the Sign in with Google Branding Guidelines (spec §8,
  // https://developers.google.com/identity/branding-guidelines, read on
  // 2026-11-21): fill #131314, stroke #8E918F 1px inside, text #E3E3E3 in
  // Google Sans Medium 14/20, 12px before the logo, 10px between the logo and
  // the text, 12px after the text; the standard colour "G" at 20px, the
  // plain SVG paths of the guidelines' HTML button generator, unchanged. The
  // states follow that generator for the dark theme. The font is bundled (a Latin and
  // Cyrillic subset under the SIL OFL 1.1, assets/fonts): the application
  // loads nothing from the network. The only colour icon of the interface.
  import googleG from "../assets/icons/google-g.svg";
  import { i18n } from "../lib/i18n.svelte";

  let { onclick, disabled = false }: { onclick: () => void; disabled?: boolean } = $props();
</script>

<button type="button" class="gsi" {onclick} {disabled}><img src={googleG} alt="" width="20" height="20" /><span>{i18n.t("account.signInGoogle")}</span></button>

<style>
  @font-face {
    font-family: "Smartnote Google Sans";
    src: url("../assets/fonts/GoogleSans-Medium-subset.woff2") format("woff2");
    font-weight: 500;
    font-style: normal;
    font-display: block;
  }

  /* The border lies inside the 40px height and the 12px paddings, as in the asset bundle. */
  .gsi {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 10px;
    box-sizing: border-box;
    height: 40px;
    padding: 0 11px;
    border: 1px solid #8e918f;
    border-radius: 4px;
    background: #131314;
    color: #e3e3e3;
    font-family: "Smartnote Google Sans", "Liberation Sans", Arial, sans-serif;
    font-size: 14px;
    font-weight: 500;
    line-height: 20px;
    white-space: nowrap;
    cursor: pointer;
    overflow: hidden;
    transition:
      background-color 0.218s,
      border-color 0.218s,
      box-shadow 0.218s;
  }
  .gsi img {
    width: 20px;
    height: 20px;
    flex: none;
  }
  /* The state layer of the generator: white over the fill, 8% on hover, 12% on focus and press. */
  .gsi::before {
    content: "";
    position: absolute;
    inset: 0;
    background: white;
    opacity: 0;
    transition: opacity 0.218s;
  }
  /* The fill stays: the global button:enabled:hover (0,2,1) outranks .gsi (0,2,0) and would grey it. */
  .gsi:enabled:hover {
    background: #131314;
    box-shadow:
      0 1px 2px 0 rgba(60, 64, 67, 0.3),
      0 1px 3px 1px rgba(60, 64, 67, 0.15);
  }
  .gsi:enabled:hover::before {
    opacity: 0.08;
  }
  .gsi:enabled:focus-visible::before,
  .gsi:enabled:active::before {
    opacity: 0.12;
  }
  .gsi:focus-visible {
    outline: 1px dotted var(--text);
    outline-offset: 2px;
  }
  .gsi:disabled {
    background: #13131461;
    border-color: #8e918f1f;
    color: #e3e3e3;
    cursor: default;
  }
  .gsi:disabled img,
  .gsi:disabled span {
    opacity: 0.38;
  }
</style>
