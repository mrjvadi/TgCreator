import { CircleAlert, Eye, EyeOff, Loader2, Plug, Plus, Server, Trash2 } from "lucide-react";
import { useState } from "react";
import { api } from "../api";
import type { XUIInbound, XUIPanel } from "../types";
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

const types: [string, string][] = [
  ["3x-ui", "3x-ui (سنایی)"],
  ["x-ui", "x-ui (علیرضا)"],
];

type Check = { state: "idle" | "busy" } | { state: "ok"; inbounds: XUIInbound[]; sub: string } | { state: "err"; msg: string };

function PanelCard({ name, p, onChange, onDelete }: { name: string; p: XUIPanel; onChange: (p: XUIPanel) => void; onDelete: () => void }) {
  const [showPass, setShowPass] = useState(false);
  const [more, setMore] = useState(!!(p.totp_secret || p.api_path || p.insecure_tls || p.timeout));
  const [check, setCheck] = useState<Check>({ state: "idle" });
  const set = (v: Partial<XUIPanel>) => {
    const next: XUIPanel = { ...p, ...v };
    // Optional settings left empty are dropped from the workflow file.
    for (const k of ["type", "totp_secret", "sub_url", "address", "api_path", "insecure_tls", "timeout"] as const) {
      if (next[k] === "" || next[k] === false) delete next[k];
    }
    onChange(next);
  };
  const test = async () => {
    setCheck({ state: "busy" });
    try {
      const r = await api.xuiCheck(p);
      setCheck(r.ok ? { state: "ok", inbounds: r.inbounds ?? [], sub: r.sub_url ?? "" } : { state: "err", msg: r.error ?? "خطای نامشخص" });
    } catch (e) {
      setCheck({ state: "err", msg: (e as Error).message });
    }
  };
  return (
    <div className="card-soft xui-card">
      <div className="db-head">
        <Server size={15} />
        <code dir="ltr">{name}</code>
        <div className="seg">
          {types.map(([k, l]) => (
            <button type="button" key={k} className={(p.type ?? "3x-ui") === k ? "on" : ""} onClick={() => set({ type: k })}>
              {l}
            </button>
          ))}
        </div>
        <button type="button" className="icon-btn danger" onClick={onDelete} aria-label="حذف پنل">
          <Trash2 size={14} />
        </button>
      </div>
      <Row label="آدرس پنل" help="آدرس کامل همراه با مسیر مخفی پنل، همان آدرسی که در مرورگر باز می‌کنید">
        <input className="input mono" dir="ltr" placeholder="https://panel.example.com:2053/secret/" value={p.url} onChange={(e) => set({ url: e.target.value })} />
      </Row>
      <div className="grid2">
        <Row label="نام کاربری">
          <input className="input mono" dir="ltr" value={p.username} onChange={(e) => set({ username: e.target.value })} />
        </Row>
        <Row label="رمز عبور" help="بهتر است داخل فایل نباشد: ${XUI_PASSWORD}">
          <div className="pass">
            <input className="input mono" dir="ltr" type={showPass ? "text" : "password"} value={p.password} onChange={(e) => set({ password: e.target.value })} />
            <button type="button" className="icon-btn" onClick={() => setShowPass(!showPass)} aria-label={showPass ? "پنهان کردن" : "نمایش"}>
              {showPass ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>
        </Row>
        <Row label="آدرس سرور در کانفیگ‌ها" help="خالی = دامنهٔ پنل">
          <input className="input mono" dir="ltr" placeholder="vpn.example.com" value={p.address ?? ""} onChange={(e) => set({ address: e.target.value })} />
        </Row>
        <Row label="آدرس پایهٔ ساب" help="خالی = از تنظیمات خود پنل">
          <input className="input mono" dir="ltr" placeholder="https://sub.example.com:2096/sub/" value={p.sub_url ?? ""} onChange={(e) => set({ sub_url: e.target.value })} />
        </Row>
      </div>
      {more ? (
        <div className="grid2">
          <Row label="کلید ورود دومرحله‌ای (TOTP)" help="اگر 2FA پنل روشن است">
            <input className="input mono" dir="ltr" value={p.totp_secret ?? ""} onChange={(e) => set({ totp_secret: e.target.value })} />
          </Row>
          <Row label="مسیر API (برای نسخه‌های دیگر)" help="خالی = پیش‌فرض نوع پنل">
            <input
              className="input mono"
              dir="ltr"
              placeholder={(p.type ?? "3x-ui") === "x-ui" ? "/xui/API/inbounds" : "/panel/api/inbounds"}
              value={p.api_path ?? ""}
              onChange={(e) => set({ api_path: e.target.value })}
            />
          </Row>
          <Row label="مهلت هر درخواست">
            <input className="input mono" dir="ltr" placeholder="15s" value={p.timeout ?? ""} onChange={(e) => set({ timeout: e.target.value })} />
          </Row>
          <div className="field">
            <Switch checked={!!p.insecure_tls} onChange={(v) => set({ insecure_tls: v })} label="پذیرش گواهی self-signed" />
          </div>
        </div>
      ) : (
        <button type="button" className="link-btn" onClick={() => setMore(true)}>
          تنظیمات بیشتر (2FA، مسیر API، گواهی)
        </button>
      )}
      <div className="xui-check">
        <button type="button" className="btn btn-soft small" onClick={test} disabled={!p.url || check.state === "busy"}>
          {check.state === "busy" ? <Loader2 size={14} className="spin" /> : <Plug size={14} />} تست اتصال
        </button>
        {check.state === "err" && (
          <span className="xui-err">
            <CircleAlert size={14} /> {check.msg}
          </span>
        )}
        {check.state === "ok" && <span className="xui-ok">✓ وصل شد — {check.inbounds.length} اینباند</span>}
      </div>
      {check.state === "ok" && check.inbounds.length > 0 && (
        <table className="xui-inbounds">
          <thead>
            <tr>
              <th>ID</th>
              <th>نام</th>
              <th>پروتکل</th>
              <th>پورت</th>
              <th>کاربر</th>
            </tr>
          </thead>
          <tbody>
            {check.inbounds.map((i) => (
              <tr key={i.id} className={i.enable ? "" : "off"}>
                <td className="mono">{i.id}</td>
                <td dir="auto">{i.remark || "—"}</td>
                <td className="mono">{i.protocol}</td>
                <td className="mono">{i.port}</td>
                <td className="mono">{i.clients}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {check.state === "ok" && check.sub && (
        <p className="field-help xui-sub">
          ساب: <code dir="ltr">{check.sub}</code>
        </p>
      )}
    </div>
  );
}

export default function XUIPanels({ value, onChange }: { value: Record<string, XUIPanel>; onChange: (v: Record<string, XUIPanel>) => void }) {
  return (
    <>
      <div className="section-title">پنل‌های X-UI (فروش و مدیریت VPN)</div>
      {Object.keys(value).length === 0 && <p className="empty-note">برای نودهای «پنل X-UI» یک پنل 3x-ui یا x-ui اضافه کنید. در «تست ربات» به‌جای پنل واقعی یک پنل شبیه‌سازی‌شده استفاده می‌شود.</p>}
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
          onChange({ ...value, [n]: { type: "3x-ui", url: "", username: "admin", password: "${XUI_PASSWORD}" } });
        }}
      >
        <Plus size={14} /> افزودن پنل X-UI
      </button>
    </>
  );
}
