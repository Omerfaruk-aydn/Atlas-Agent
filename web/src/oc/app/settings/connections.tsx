import { Button } from "@atlas/ui/button"
import { Icon } from "@atlas/ui/icon"
import { TextInput } from "@atlas/ui/text-input"
import { createResource, createSignal, For, Show } from "solid-js"
import { api } from "../../../api/client"
import type { ConnectionSave, ConnectionView } from "../../../api/types"
import { t as at } from "../../../i18n"
import { showToast } from "../../toast"

export function Connections(props: { workspaceID: string }) {
  const [connections, { mutate }] = createResource(() => props.workspaceID, api.connections)
  const available = () => (connections.latest ?? []).filter((item) => item.available)
  const planned = () => (connections.latest ?? []).filter((item) => !item.available)
  return (
    <>
      <div class="settings-tab-header">
        <div class="settings-tab-header-row">
          <div class="flex flex-col gap-1">
            <h2 class="settings-tab-title">{at("settingsNav.connections")}</h2>
            <span class="text-11-regular text-v2-text-text-muted">{at("settings.connectionsDescription")}</span>
          </div>
        </div>
      </div>
      <div class="settings-tab-body settings-connections">
        <Show when={connections.latest} fallback={<p class="settings-connection-note">{at("common.loading")}</p>}>
          <div class="settings-connection-security">
            <Icon name="lock" size="small" />
            <span>{at("settings.connectionsSecurity")}</span>
          </div>
          <For each={available()}>
            {(item) => <ConnectionCard item={item} workspaceID={props.workspaceID} onSaved={mutate} />}
          </For>
          <section class="settings-connection-catalog">
            <h3>{at("settings.connectionsMore")}</h3>
            <p>{at("settings.connectionsMoreDescription")}</p>
            <div class="settings-connection-planned">
              <For each={planned()}>{(item) => <span>{item.name}</span>}</For>
            </div>
          </section>
        </Show>
      </div>
    </>
  )
}

function Field(props: { label: string; value: string; secret?: boolean; saved?: boolean; placeholder?: string; onInput: (value: string) => void }) {
  return (
    <label class="settings-connection-field">
      <span>{props.label}</span>
      <TextInput
        appearance="base"
        type={props.secret ? "password" : "text"}
        value={props.value}
        placeholder={props.saved ? at("settings.connectionSecretSaved") : props.placeholder}
        autocomplete={props.secret ? "new-password" : "off"}
        spellcheck={false}
        onInput={(event) => props.onInput(event.currentTarget.value)}
      />
    </label>
  )
}

function ConnectionCard(props: { item: ConnectionView; workspaceID: string; onSaved: (views: ConnectionView[]) => void }) {
  const field = (name: string) => props.item.fields?.[name] ?? ""
  const [apiKey, setAPIKey] = createSignal("")
  const [voiceID, setVoiceID] = createSignal(field("voice_id"))
  const [sttModel, setSTTModel] = createSignal(field("stt_model"))
  const [ttsModel, setTTSModel] = createSignal(field("tts_model"))
  const [accountSID, setAccountSID] = createSignal("")
  const [authToken, setAuthToken] = createSignal("")
  const [phoneNumber, setPhoneNumber] = createSignal(field("phone_number"))
  const [ownerNumber, setOwnerNumber] = createSignal(field("owner_number"))
  const [whatsAppFrom, setWhatsAppFrom] = createSignal(field("whatsapp_from"))
  const [publicBaseURL, setPublicBaseURL] = createSignal(field("public_base_url"))
  const [voiceEnabled, setVoiceEnabled] = createSignal(field("voice_enabled") === "true")
  const [botToken, setBotToken] = createSignal("")
  const [chatID, setChatID] = createSignal(field("chat_id"))
  const [clientID, setClientID] = createSignal("")
  const [clientSecret, setClientSecret] = createSignal("")
  const [token, setToken] = createSignal("")
  const [busy, setBusy] = createSignal<"save" | "test" | "oauth" | "refresh" | undefined>()
  const [message, setMessage] = createSignal("")

  const body = (): ConnectionSave => {
    if (props.item.id === "elevenlabs") return { api_key: apiKey(), voice_id: voiceID(), stt_model: sttModel(), tts_model: ttsModel() }
    if (props.item.id === "twilio")
      return {
        account_sid: accountSID(), auth_token: authToken(), phone_number: phoneNumber(), owner_number: ownerNumber(),
        whatsapp_from: whatsAppFrom(), public_base_url: publicBaseURL(), voice_enabled: voiceEnabled(),
      }
    if (["google", "outlook", "slack", "github", "notion"].includes(props.item.id)) return { client_id: clientID(), client_secret: clientSecret() }
    if (props.item.id === "discord") return { token: token() }
    return { bot_token: botToken(), chat_id: chatID() }
  }
  const save = async () => {
    setBusy("save")
    setMessage("")
    try {
      props.onSaved(await api.saveConnection(props.workspaceID, props.item.id, body()))
      setAPIKey("")
      setAccountSID("")
      setAuthToken("")
      setBotToken("")
      setClientSecret("")
      setToken("")
      setMessage(at("settings.connectionSaved"))
    } catch (error) {
      showToast({ variant: "error", title: at("settingsNav.connections"), description: error instanceof Error ? error.message : String(error) })
    } finally {
      setBusy(undefined)
    }
  }
  const test = async () => {
    setBusy("test")
    setMessage("")
    try {
      const result = await api.testConnection(props.workspaceID, props.item.id)
      setMessage(result.message)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(undefined)
    }
  }
  const authorizeElevenLabs = async () => {
    const popup = window.open("about:blank", "_blank")
    if (!popup) {
      setMessage(at("settings.connectionPopupBlocked"))
      return
    }
    setBusy("oauth")
    setMessage("")
    try {
      const result = await api.startConnectionOAuth(props.workspaceID, props.item.id)
      if (result.auth_url) popup.location.assign(result.auth_url)
      const deadline = Date.now() + 10 * 60 * 1000
      while (Date.now() < deadline) {
        const views = await api.connections(props.workspaceID)
        props.onSaved(views)
        const current = views.find((view) => view.id === "elevenlabs")
        if (current?.fields?.oauth_state === "connected") {
          popup.close()
          setMessage(at("settings.connectionElevenLabsConnected"))
          return
        }
        if (current?.fields?.oauth_error) throw new Error(current.fields.oauth_error)
        if (popup.closed) {
          setMessage(at("settings.connectionOAuthStarted"))
          return
        }
        await new Promise((resolve) => setTimeout(resolve, 1500))
      }
      setMessage(at("settings.connectionOAuthStarted"))
    } catch (error) {
      popup.close()
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(undefined)
    }
  }
  const authorize = async () => {
    if (props.item.id === "elevenlabs") {
      await authorizeElevenLabs()
      return
    }
    setBusy("oauth")
    setMessage("")
    try {
      const result = await api.startConnectionOAuth(props.workspaceID, props.item.id)
      window.location.assign(result.auth_url)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error))
      setBusy(undefined)
    }
  }
  const refresh = async () => {
    setBusy("refresh")
    try {
      props.onSaved(await api.connections(props.workspaceID))
      setMessage(at("settings.connectionRefreshed"))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(undefined)
    }
  }

  return (
    <section class="settings-connection-card" data-connection={props.item.id}>
      <header class="settings-connection-header">
        <div>
          <h3>{props.item.name}</h3>
          <p>{props.item.description}</p>
        </div>
        <span class="settings-connection-status" data-configured={props.item.configured}>
          {props.item.configured ? at("settings.connectionConfigured") : at("settings.connectionNotConfigured")}
        </span>
      </header>
      <div class="settings-connection-fields">
        <Show when={props.item.id === "elevenlabs"}>
          <p class="settings-connection-note">{at("settings.connectionElevenLabsHelp")}</p>
          <details class="settings-connection-advanced">
            <summary>{at("settings.connectionElevenLabsAPISettings")}</summary>
            <p class="settings-connection-note">{at("settings.connectionElevenLabsAPIHelp")}</p>
            <Field label="API key" value={apiKey()} secret saved={props.item.secrets?.api_key} onInput={setAPIKey} />
            <Field label="Voice ID" value={voiceID()} onInput={setVoiceID} />
            <div class="settings-connection-field-pair">
              <Field label="STT model" value={sttModel()} onInput={setSTTModel} />
              <Field label="TTS model" value={ttsModel()} onInput={setTTSModel} />
            </div>
            <Button size="normal" variant="outline" disabled={busy() !== undefined} onClick={save}>
              {busy() === "save" ? at("common.loading") : at("common.save")}
            </Button>
          </details>
        </Show>
        <Show when={props.item.id === "twilio"}>
          <div class="settings-connection-field-pair">
            <Field label="Account SID" value={accountSID()} secret saved={props.item.secrets?.account_sid} onInput={setAccountSID} />
            <Field label="Auth token" value={authToken()} secret saved={props.item.secrets?.auth_token} onInput={setAuthToken} />
          </div>
          <div class="settings-connection-field-pair">
            <Field label={at("settings.connectionTwilioNumber")} value={phoneNumber()} placeholder="+905551112233" onInput={setPhoneNumber} />
            <Field label={at("settings.connectionOwnerNumber")} value={ownerNumber()} placeholder="+905551112233" onInput={setOwnerNumber} />
          </div>
          <Field label={at("settings.connectionWhatsAppNumber")} value={whatsAppFrom()} placeholder="+14155238886" onInput={setWhatsAppFrom} />
          <Field label={at("settings.connectionPublicURL")} value={publicBaseURL()} placeholder="https://…" onInput={setPublicBaseURL} />
          <label class="settings-connection-toggle">
            <input type="checkbox" checked={voiceEnabled()} onChange={(event) => setVoiceEnabled(event.currentTarget.checked)} />
            <span>{at("settings.connectionVoiceEnabled")}</span>
          </label>
        </Show>
        <Show when={props.item.id === "telegram"}>
          <Field label="Bot token" value={botToken()} secret saved={props.item.secrets?.bot_token} onInput={setBotToken} />
          <Field label="Chat ID" value={chatID()} onInput={setChatID} />
        </Show>
        <Show when={props.item.id === "google"}>
          <p class="settings-connection-note">{at("settings.connectionGoogleHelp")}</p>
          <div class="settings-connection-field-pair">
            <Field label="OAuth Client ID" value={clientID()} secret saved={props.item.secrets?.client_id} onInput={setClientID} />
            <Field label="OAuth Client Secret" value={clientSecret()} secret saved={props.item.secrets?.client_secret} onInput={setClientSecret} />
          </div>
        </Show>
        <Show when={["slack", "github", "notion"].includes(props.item.id)}>
          <p class="settings-connection-note">{at(`settings.connection${props.item.id}Help` as never)}</p>
          <div class="settings-connection-field-pair">
            <Field label="OAuth Client ID" value={clientID()} secret saved={props.item.secrets?.client_id} onInput={setClientID} />
            <Field label="OAuth Client Secret" value={clientSecret()} secret saved={props.item.secrets?.client_secret} onInput={setClientSecret} />
          </div>
        </Show>
        <Show when={props.item.id === "discord"}>
          <p class="settings-connection-note">{at("settings.connectiondiscordHelp")}</p>
          <Field label={at("settings.connectionToken")} value={token()} secret saved={props.item.secrets?.token} onInput={setToken} />
        </Show>
        <Show when={props.item.id === "outlook"}>
          <p class="settings-connection-note">{at("settings.connectionOutlookHelp")}</p>
          <div class="settings-connection-field-pair">
            <Field label="Application (Client) ID" value={clientID()} secret saved={props.item.secrets?.client_id} onInput={setClientID} />
            <Field label="Client Secret (optional)" value={clientSecret()} secret saved={props.item.secrets?.client_secret} onInput={setClientSecret} />
          </div>
        </Show>
      </div>
      <footer class="settings-connection-actions">
        <Show when={message()}><span role="status">{message()}</span></Show>
        <Show when={["elevenlabs", "google", "outlook", "slack", "github", "notion"].includes(props.item.id)}>
          <Button size="normal" variant="outline" disabled={busy() !== undefined} onClick={refresh}>{at("common.refresh")}</Button>
          <Button size="normal" variant="contrast" disabled={busy() !== undefined} onClick={authorize}>
            {busy() === "oauth" ? at("settings.connectionOAuthWaiting") : at("settings.connectionOAuthConnect")}
          </Button>
        </Show>
        <Button size="normal" variant="outline" disabled={busy() !== undefined} onClick={test}>
          {busy() === "test" ? at("settings.connectionTesting") : at("settings.connectionTest")}
        </Button>
        <Show when={props.item.id !== "elevenlabs"}>
          <Button size="normal" variant="contrast" disabled={busy() !== undefined} onClick={save}>
            {busy() === "save" ? at("common.loading") : at("common.save")}
          </Button>
        </Show>
      </footer>
    </section>
  )
}
