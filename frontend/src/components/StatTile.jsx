// A stat box. Optionally shows a delta chip (e.g. "▲ 34%") whose tone colors it:
// "up" = worse (higher latency, danger red), "down" = better (accent green).
export default function StatTile({ label, value, sub, deltaText, deltaTone }) {
  return (
    <div className="tile">
      <div className="label">{label}</div>
      <div className="value-row">
        <span className="value">{value}</span>
        {deltaText && <span className={`delta ${deltaTone || 'flat'}`}>{deltaText}</span>}
      </div>
      {sub && <div className="sub">{sub}</div>}
    </div>
  )
}
