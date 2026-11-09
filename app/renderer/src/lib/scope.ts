// Where a thread is shown. A side chat has no edit or rewind: those fork the
// open chat, and the side chat is not that.
import { createContext } from "react";

export const SideScope = createContext(false);
