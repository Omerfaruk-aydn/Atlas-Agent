/* @refresh reload */
import { render } from "solid-js/web"
import "./styles/atlas-app.css"
import "./styles/atlas.css"
import { App } from "./App"
import "./state/prefs"

render(() => <App />, document.getElementById("root")!)
