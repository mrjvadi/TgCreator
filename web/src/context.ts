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
