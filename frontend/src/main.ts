import { mount } from 'svelte'
import App from './App.svelte'
import { PrefsService } from './lib/api'
import { i18n } from './lib/i18n.svelte'

// The first screen shows in the saved language, or in the system's while none is saved (spec §16.4).
i18n.use(PrefsService)
void i18n.start(navigator.language).finally(() => mount(App, { target: document.getElementById('app')! }))
