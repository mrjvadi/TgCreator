import { createContext } from "react";
import type { MetaMap } from "./convert";
import type { BotMethod } from "./types";

export interface CatalogState {
  metas: MetaMap;
  methods: BotMethod[];
  methodMap: Record<string, BotMethod>;
  botApi: string;
}

export const CatalogContext = createContext<CatalogState>({ metas: {}, methods: [], methodMap: {}, botApi: "" });

/** Actions nodes can trigger on the editor (the "+" after an output). */
export interface EditorActions {
  addAfter: (source: string, output: string) => void;
}

export const EditorContext = createContext<EditorActions>({ addAfter: () => {} });
