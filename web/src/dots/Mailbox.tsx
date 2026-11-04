// The owner's mailbox. The address and secret are saved encrypted and
// tested with a real login. The chat never sees the secret.

import { createResource, createSignal, Show } from "solid-js"
import { api } from "../api/client"
import { toast } from "./store"

export function Mailbox(props: { workspaceId: string }) {
  const [address, setAddress] = createSignal("")
  const [secret, setSecret] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [saved, { refetch }] = createResource(
    () => props.workspaceId,
    async (id) => {
      const views = await api.connections(id)
      return views.find((view) => view.id === "mailbox")
    },
  )

  const save = async () => {
    if (busy()) return
    setBusy(true)
    try {
      await api.saveConnection(props.workspaceId, "mailbox", { mail_address: address().trim(), mail_secret: secret() })
      setSecret("")
      await refetch()
      toast("Posta bağlandı")
    } catch (err) {
      toast((err as Error).message, true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="group" style={{ "margin-top": "14px" }}>
      <div class="group-row">
        <div class="text">
          <b>Posta</b>
          <small>
            <Show when={saved()?.configured} fallback="Gmail adresini ve şifresini buraya yaz. Sohbete düşmez. Ajan kodu bu kutudan okur.">
              {saved()?.fields?.address} bağlı. Yeni bir şifre yazınca eskisinin yerini alır.
            </Show>
          </small>
        </div>
      </div>
      <div class="group-row" style={{ display: "grid", gap: "8px" }}>
        <input class="input" type="email" autocomplete="off" placeholder="ad@gmail.com" value={address()} onInput={(e) => setAddress(e.currentTarget.value)} />
        <input class="input" type="password" autocomplete="new-password" placeholder={saved()?.secrets?.mail_secret ? "Yeni şifre" : "Şifre veya uygulama şifresi"} value={secret()} onInput={(e) => setSecret(e.currentTarget.value)} />
        <button class="btn sm" disabled={busy() || !address().trim() || !secret().trim()} onClick={() => void save()}>
          {busy() ? "Deneniyor…" : "Bağla"}
        </button>
      </div>
    </div>
  )
}
