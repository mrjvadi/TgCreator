import { CircleAlert, Eye, EyeOff, Loader2, Plug, Plus, Server, Trash2 } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import type { VPNGroup, VPNPanel } from "../types";
import { Switch } from "./Inspector";

function Row({ label, help, children }: { label: string; help?: string; children: React.ReactNode }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {help && <span className="field-help">{help}</span>}
    </label>
  );
}

type Kind = "xui" | "marzban" | "remnawave" | "hiddify";

// What each panel needs; ids match the runtime's workflow.VPNTypes.
const types: { id: string; name: string; kind: Kind; url: string; urlHelp: string; groups: string }[] = [
  { id: "3x-ui", name: "3x-ui (سنایی)", kind: "xui", url: "https://panel.example.com:2053/secret/", urlHelp: "آدرس کامل همراه با مسیر مخفی پنل، همان که در مرورگر باز می‌کنید", groups: "اینباندها" },
  { id: "x-ui", name: "x-ui (علیرضا)", kind: "xui", url: "https://panel.example.com:54321/secret/", urlHelp: "آدرس کامل همراه با مسیر مخفی پنل", groups: "اینباندها" },
  { id: "marzban", name: "مرزبان", kind: "marzban", url: "https://panel.example.com:8000/", urlHelp: "آدرس پنل (مسیر /dashboard لازم نیست)", groups: "اینباندها (تگ)" },
  { id: "pasarguard", name: "پاسارگاد", kind: "marzban", url: "https://panel.example.com:8000/", urlHelp: "آدرس پنل (مسیر /dashboard لازم نیست)", groups: "گروه‌ها" },
  { id: "marzneshin", name: "مرزنشین", kind: "marzban", url: "https://panel.example.com:8000/", urlHelp: "آدرس پنل (مسیر /dashboard لازم نیست)", groups: "سرویس‌ها" },
  { id: "remnawave", name: "رمناویو", kind: "remnawave", url: "https://panel.example.com/", urlHelp: "آدرس پنل", groups: "اسکوادها" },
  { id: "hiddify", name: "هیدیفای", kind: "hiddify", url: "https://example.com/ADMIN_PATH/ADMIN_UUID/admin/", urlHelp: "لینک کامل پنل ادمین؛ کلید API از خود لینک خوانده می‌شود", groups: "" },
];

type Check = { state: "idle" | "busy" } | { state: "ok"; groups: VPNGroup[] } | { state: "err"; msg: string };

function PanelCard({ name, p, onChange, onDelete }: { name: string; p: VPNPanel; onChange: (p: VPNPanel) => void; onDelete: () => void }) {
  const t = types.find((x) => x.id === (p.type ?? "3x-ui")) ?? types[0];
  const [showPass, setShowPass] = useState(false);
  const [more, setMore] = useState(!!(p.totp_secret || p.api_path || p.insecure_tls || p.timeout));
  const [check, setCheck] = useState<Check>({ state: "idle" });
  const set = (v: Partial<VPNPanel>) => {
    const next: VPNPanel = { ...p, ...v };
    // Settings left empty are dropped from the workflow file.
    for (const k of ["username", "password", "token", "totp_secret", "sub_url", "address", "api_path", "insecure_tls", "timeout"] as const) {
      if (next[k] === "" || next[k] === false) delete next[k];
    }
    onChange(next);
    setCheck({ state: "idle" });
  };
  // Credentials do not carry over between panel kinds (a Remnawave token
  // is not a Hiddify key); hidden leftovers would be sent silently.
  const changeType = (type: string) => {
    const nt = types.find((x) => x.id === type) ?? types[0];
    const next: VPNPanel = { ...p, type };
    if (nt.kind !== t.kind) {
      delete next.token;
      if (nt.kind === "remnawave" || nt.kind === "hiddify") {
        delete next.username;
        delete next.password;
      }
    }
    onChange(next);
    setCheck({ state: "idle" });
  };
  const test = async () => {
    setCheck({ state: "busy" });
    try {
      const r = await api.vpnCheck(p);
      setCheck(r.ok ? { state: "ok", groups: r.groups ?? [] } : { state: "err", msg: r.error ?? "خطای نامشخص" });
    } catch (e) {
      setCheck({ state: "err", msg: (e as Error).message });
    }
  };
  const secret = (label: string, key: "password" | "token", help?: string) => (
    <Row label={label} help={help}>
      <div className="pass">
        <input className="input mono" dir="ltr" type={showPass ? "text" : "password"} value={p[key] ?? ""} onChange={(e) => set({ [key]: e.target.value })} />
        <button type="button" className="icon-btn" onClick={() => setShowPass(!showPass)} aria-label={showPass ? "پنهان کردن" : "نمایش"}>
          {showPass ? <EyeOff size={14} /> : <Eye size={14} />}
        </button>
      </div>
    </Row>
  );
  return (
    <div className="card-soft vpn-card">
      <div className="db-head">
        <Server size={15} />
        <code dir="ltr">{name}</code>
        <select className="input vpn-type" value={t.id} onChange={(e) => changeType(e.target.value)} aria-label="نوع پنل">
          {types.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </select>
        <button type="button" className="icon-btn danger" onClick={onDelete} aria-label="حذف پنل">
          <Trash2 size={14} />
        </button>
      </div>
      <Row label={t.kind === "hiddify" ? "لینک پنل ادمین" : "آدرس پنل"} help={t.urlHelp}>
        <input className="input mono" dir="ltr" placeholder={t.url} value={p.url} onChange={(e) => set({ url: e.target.value })} />
      </Row>
      <div className="grid2">
        {(t.kind === "xui" || t.kind === "marzban") && (
          <>
            <Row label="نام کاربری ادمین">
              <input className="input mono" dir="ltr" value={p.username ?? ""} onChange={(e) => set({ username: e.target.value })} />
            </Row>
            {secret("رمز عبور", "password", "بهتر است داخل فایل نباشد: ${VPN_PASSWORD}")}
          </>
        )}
        {t.kind === "remnawave" && secret("توکن API", "token", "تنظیمات پنل ← API Tokens؛ مثلاً ${VPN_TOKEN}")}
        {t.kind === "hiddify" && secret("کلید API (UUID ادمین)", "token", "اگر در لینک بالا هست خالی بگذارید")}
        {t.kind === "xui" && (
          <Row label="آدرس سرور در کانفیگ‌ها" help="خالی = دامنهٔ پنل">
            <input className="input mono" dir="ltr" placeholder="vpn.example.com" value={p.address ?? ""} onChange={(e) => set({ address: e.target.value })} />
          </Row>
        )}
        <Row label="آدرس پایهٔ ساب" help={t.kind === "hiddify" ? "لینک کاربر: https://domain/CLIENT_PATH/ — بدون آن لینک ساب ساخته نمی‌شود" : "خالی = همان که پنل می‌دهد"}>
          <input
            className="input mono"
            dir="ltr"
            placeholder={t.kind === "hiddify" ? "https://example.com/CLIENT_PATH/" : "https://sub.example.com/sub/"}
            value={p.sub_url ?? ""}
            onChange={(e) => set({ sub_url: e.target.value })}
          />
        </Row>
      </div>
      {more ? (
        <div className="grid2">
          {t.kind === "xui" && (
            <>
              <Row label="کلید ورود دومرحله‌ای (TOTP)" help="اگر 2FA پنل روشن است">
                <input className="input mono" dir="ltr" value={p.totp_secret ?? ""} onChange={(e) => set({ totp_secret: e.target.value })} />
              </Row>
              <Row label="مسیر API (برای نسخه‌های دیگر)" help="خالی = پیش‌فرض نوع پنل">
                <input className="input mono" dir="ltr" placeholder={t.id === "x-ui" ? "/xui/API/inbounds" : "/panel/api/inbounds"} value={p.api_path ?? ""} onChange={(e) => set({ api_path: e.target.value })} />
              </Row>
            </>
          )}
          <Row label="مهلت هر درخواست">
            <input className="input mono" dir="ltr" placeholder="15s" value={p.timeout ?? ""} onChange={(e) => set({ timeout: e.target.value })} />
          </Row>
          <div className="field">
            <Switch checked={!!p.insecure_tls} onChange={(v) => set({ insecure_tls: v })} label="پذیرش گواهی self-signed" />
          </div>
        </div>
      ) : (
        <button type="button" className="link-btn" onClick={() => setMore(true)}>
          تنظیمات بیشتر
        </button>
      )}
      <div className="vpn-check">
        <button type="button" className="btn btn-soft small" onClick={test} disabled={!p.url || check.state === "busy"}>
          {check.state === "busy" ? <Loader2 size={14} className="spin" /> : <Plug size={14} />} تست اتصال
        </button>
        {check.state === "err" && (
          <span className="vpn-err">
            <CircleAlert size={14} /> {check.msg}
          </span>
        )}
        {check.state === "ok" && <span className="vpn-ok">✓ وصل شد{t.groups ? ` — ${check.groups.length} ${t.groups}` : ""}</span>}
      </div>
      {check.state === "ok" && check.groups.length > 0 && (
        <table className="vpn-inbounds">
          <thead>
            <tr>
              <th>{t.kind === "marzban" && t.id === "marzban" ? "تگ" : "شناسه"}</th>
              <th>نام</th>
              {t.kind === "xui" || t.id === "marzban" ? (
                <>
                  <th>پروتکل</th>
                  <th>پورت</th>
                </>
              ) : null}
              {t.id !== "marzban" && <th>کاربر</th>}
            </tr>
          </thead>
          <tbody>
            {check.groups.map((g) => (
              <tr key={g.id} className={g.enabled ? "" : "off"}>
                <td className="mono" dir="ltr">
                  {g.id}
                </td>
                <td dir="auto">{g.name || "—"}</td>
                {t.kind === "xui" || t.id === "marzban" ? (
                  <>
                    <td className="mono">{g.protocol}</td>
                    <td className="mono">{g.port || ""}</td>
                  </>
                ) : null}
                {t.id !== "marzban" && <td className="mono">{g.users}</td>}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {check.state === "ok" && t.groups && (
        <p className="field-help vpn-sub">در نود «ساخت کاربر VPN»، فیلد «اینباند / گروه» همین شناسه‌ها را می‌گیرد{t.kind === "xui" ? " (اجباری)" : "؛ خالی = همه"}.</p>
      )}
    </div>
  );
}

export default function VPNPanels({ value, onChange }: { value: Record<string, VPNPanel>; onChange: (v: Record<string, VPNPanel>) => void }) {
  return (
    <>
      <div className="section-title">پنل‌های VPN (X-UI، مرزبان، پاسارگاد، مرزنشین، رمناویو، هیدیفای)</div>
      {Object.keys(value).length === 0 && <p className="empty-note">برای نودهای «پنل VPN» یک پنل اضافه کنید. در «تست ربات» به‌جای پنل واقعی یک پنل شبیه‌سازی‌شده از همان نوع استفاده می‌شود.</p>}
      {Object.entries(value).map(([name, p]) => (
        <PanelCard
          key={name}
          name={name}
          p={p}
          onChange={(np) => onChange({ ...value, [name]: np })}
          onDelete={() => {
            const next = { ...value };
            delete next[name];
            onChange(next);
          }}
        />
      ))}
      <button
        type="button"
        className="add-line"
        onClick={() => {
          let n = Object.keys(value).length ? `panel${Object.keys(value).length + 1}` : "main";
          while (value[n]) n += "_";
          onChange({ ...value, [n]: { type: "3x-ui", url: "", username: "admin", password: "${VPN_PASSWORD}" } });
        }}
      >
        <Plus size={14} /> افزودن پنل VPN
      </button>
    </>
  );
}
