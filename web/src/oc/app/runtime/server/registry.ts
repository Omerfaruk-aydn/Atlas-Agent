// Atlas Agent can talk to several servers; Atlas has exactly one, the server
// this web UI is connected to. The copied views keep their multi-server
// types, and this module describes the single connection.

export namespace ServerConnection {
  export type Key = string & { readonly __serverKey?: true }
  export type Http = {
    type: "http"
    http: { url: string; password?: string }
    displayName?: string
    label?: string
    authenticationRequired?: undefined
    connecting?: undefined
  }
  export type Extension = {
    type: "extension"
    http: { url: string }
    displayName?: string
    label?: string
    authenticationRequired?: boolean
    connecting?: boolean
  }
  export type Any = Http | Extension
  export const key = (conn: Any): Key => conn.http.url as Key
  export const builtin = (_conn: Any) => true
}

export function serverName(conn: ServerConnection.Any, _full?: boolean) {
  return conn.displayName ?? new URL(conn.http.url).host
}

/** The Atlas server this page talks to, named by the gateway's origin. */
export const atlasServer: ServerConnection.Http = { type: "http", http: { url: location.origin }, displayName: "Atlas" }

export function useServers() {
  return { list: [atlasServer], visible: [atlasServer], hydrated: () => true }
}
