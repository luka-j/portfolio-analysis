import { useState, useMemo } from 'react'
import {
  LineChart, Line, AreaChart, Area, ComposedChart,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'
import type { LegendPayload, ContentType } from 'recharts/types/component/DefaultLegendContent'
import type { ValueType, NameType } from 'recharts/types/component/DefaultTooltipContent'
import type { ReactNode } from 'react'
import { RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../../utils/format'
import type { ChartMode } from '../../pages/hooks/types'

const COLORS = ['#818cf8', '#34d399', '#fbbf24', '#f87171', '#22d3ee', '#f472b6', '#a78bfa']
const COMPARE_COLOR = '#fbbf24'

const AXIS_STYLE = { fontSize: 10, fill: '#475569' }
const AXIS_LABEL_STYLE = { fontSize: 10, fill: '#334155', fontWeight: 900 }

function xTickFormatter(val: string | number) {
  return new Date(val).toLocaleString('default', { month: 'short', year: '2-digit' })
}

// labelFormatter receives ReactNode from recharts; we know at runtime it's always a numeric timestamp.
const tooltipLabelFormatter = (label: ReactNode): ReactNode => {
  if (!label) return '';
  const d = new Date(Number(label));
  return isNaN(d.getTime()) ? String(label) : d.toLocaleDateString('en-CA');
};

const COMMON_X_AXIS_PROPS = {
  dataKey: "timestamp",
  type: "number" as const,
  scale: "time" as const,
  domain: ['dataMin', 'dataMax'] as const,
  tickFormatter: xTickFormatter,
  minTickGap: 60,
  tick: AXIS_STYLE,
  axisLine: { stroke: '#2a2e42' },
  tickLine: false,
  label: { value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE } as Record<string, unknown>
};

const COMMON_Y_AXIS_PROPS = {
  tick: AXIS_STYLE,
  axisLine: false,
  tickLine: false,
  width: 56,
};

const COMMON_GRID_PROPS = {
  strokeDasharray: "3 3",
  stroke: "#2a2e42",
  vertical: false,
  opacity: 0.3
};

const COMMON_TOOLTIP_PROPS = {
  contentStyle: RECHARTS_TOOLTIP_STYLE,
  labelStyle: RECHARTS_LABEL_STYLE,
  itemStyle: RECHARTS_ITEM_STYLE,
  labelFormatter: tooltipLabelFormatter
};

// Chart rows are plain objects with a 'date' string key plus arbitrary numeric series keys,
// plus number[] tuples for ghost-area ranges added by processAllCompareRanges.
// Using Record<string, unknown> so strongly-typed API structs (DailyValue, RollingPoint, etc.)
// are assignable here without requiring an explicit index signature on each API type.
type ChartRow = Record<string, unknown>

export interface PerformanceChartProps {
  chartMode: ChartMode
  mergedChartData: ChartRow[]
  mwrChartData: ChartRow[]
  drawdownChartData: ChartRow[]
  rollingChartData: ChartRow[]
  rollingMetricLabel: string
  compareLabel: string | null
  compareTwrHistory: ChartRow[]
  benchmarkSymbols: string[]
  scenarioBenchmarks: { name: string }[]
  rollingSeries: Record<string, ChartRow[]>
}

function processAllCompareRanges(
  data: ChartRow[],
  portfolioKey: string,
  compareKeys: string[],
  invert = false,
): ChartRow[] {
  if (data.length === 0 || compareKeys.length === 0) return data;

  let result = [...data];

  for (const compareKey of compareKeys) {
    const temp: ChartRow[] = [];
    for (let i = 0; i < result.length; i++) {
      const curr = result[i];
      if (i > 0) {
        const prev = result[i - 1];
        const p1 = prev[portfolioKey], c1 = prev[compareKey];
        const p2 = curr[portfolioKey], c2 = curr[compareKey];

        if (typeof p1 === 'number' && typeof c1 === 'number' && typeof p2 === 'number' && typeof c2 === 'number') {
          if ((p1 > c1 && p2 < c2) || (p1 < c1 && p2 > c2)) {
            const t = (c1 - p1) / ((p2 - p1) - (c2 - c1));
            const prevTime = Number(prev.timestamp);
            const currTime = Number(curr.timestamp);

            const crossDateObj = new Date(prevTime + t * (currTime - prevTime));

            const crossData: ChartRow = {
              date: crossDateObj.toISOString(),
              timestamp: crossDateObj.getTime()
            };
            for (const key of Object.keys(prev)) {
              if (key === 'date' || key === 'timestamp') continue;
              const pv = prev[key], cv = curr[key];
              if (typeof pv === 'number' && typeof cv === 'number') {
                crossData[key] = pv + t * (cv - pv);
              } else {
                crossData[key] = pv;
              }
            }
            temp.push(crossData);
          }
        }
      }
      temp.push(curr);
    }
    result = temp;
  }

  return result.map(d => {
    const newD: ChartRow = { ...d };
    const p = d[portfolioKey];
    for (const compareKey of compareKeys) {
      const c = d[compareKey];
      if (typeof p === 'number' && typeof c === 'number') {
        const isOutperforming = invert ? p <= c : p >= c;
        const top = Math.max(p, c);
        const bottom = Math.min(p, c);
        newD[`Out_${compareKey}`] = isOutperforming ? [bottom, top] : [c, c];
        newD[`Under_${compareKey}`] = !isOutperforming ? [bottom, top] : [c, c];
      }
    }
    return newD;
  });
}

export default function PerformanceChart({
  chartMode,
  mergedChartData,
  mwrChartData,
  drawdownChartData,
  rollingChartData,
  rollingMetricLabel,
  compareLabel,
  compareTwrHistory,
  benchmarkSymbols,
  scenarioBenchmarks,
  rollingSeries
}: PerformanceChartProps) {
  const [hiddenSeries, setHiddenSeries] = useState<Record<string, boolean>>({})

  const availableCompareKeys = useMemo(() => [
    ...(compareLabel && compareTwrHistory.length > 0 ? ['Compare'] : []),
    ...benchmarkSymbols,
    ...scenarioBenchmarks.map(sb => `[S] ${sb.name}`)
  ], [compareLabel, benchmarkSymbols, scenarioBenchmarks, compareTwrHistory.length]);

  const [explicitCompareKey, setExplicitCompareKey] = useState<string | null>(null);
  const [hoveredCompareKey, setHoveredCompareKey] = useState<string | null>(null);

  const toggleSeries = (entry: LegendPayload) => {
    const key = entry?.dataKey;
    if (key !== undefined) {
      const strKey = String(key);
      setHiddenSeries(prev => ({
        ...prev,
        [strKey]: !prev[strKey]
      }))
    }
  }

  const handleLegendDoubleClick = (dataKey: string) => {
    if (explicitCompareKey === dataKey || (explicitCompareKey === null && availableCompareKeys.length === 1 && availableCompareKeys[0] === dataKey)) {
      setExplicitCompareKey('');
    } else {
      setExplicitCompareKey(dataKey);
    }
  };

  const activeCompareKey = hoveredCompareKey || (explicitCompareKey !== null ? (explicitCompareKey === '' ? null : explicitCompareKey) : (availableCompareKeys.length === 1 ? availableCompareKeys[0] : null));

  // Typed to match recharts ContentType: (props: Props) => ReactNode
  const renderCustomLegend: ContentType = (props: { payload?: ReadonlyArray<LegendPayload> }) => {
    const { payload } = props;
    if (!payload) return null;

    return (
      <ul className="flex flex-wrap justify-center gap-4 mt-6 text-[10px] text-slate-500 font-black uppercase tracking-[0.15em] cursor-pointer select-none">
        {payload.map((entry: LegendPayload, index: number) => {
          const dk = entry.dataKey !== undefined ? String(entry.dataKey) : '';
          if (dk.startsWith('Out_') || dk.startsWith('Under_')) return null;

          const isHidden = hiddenSeries[dk];
          const isCompareItem = dk !== 'Portfolio' && dk !== 'Drawdown';
          const isActiveTarget = isCompareItem && dk === activeCompareKey && !isHidden;

          return (
            <li
              key={`item-${index}`}
              className={`flex items-center gap-1.5 transition-all ${isHidden ? 'opacity-40 grayscale' : 'opacity-100'} ${isActiveTarget ? 'text-slate-100 scale-105' : 'hover:text-slate-300'}`}
              onClick={() => toggleSeries(entry)}
              onDoubleClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                if (isCompareItem && !isHidden) handleLegendDoubleClick(dk);
              }}
              onMouseEnter={() => isCompareItem && !isHidden && setHoveredCompareKey(dk)}
              onMouseLeave={() => isCompareItem && setHoveredCompareKey(null)}
            >
              <span className={`w-2.5 h-2.5 rounded-full ${isActiveTarget ? 'ring-2 ring-indigo-400/50' : ''}`} style={{ backgroundColor: entry.color }} />
              <span className={isActiveTarget ? 'underline underline-offset-4 decoration-indigo-400/50' : ''}>{entry.value}</span>
            </li>
          );
        })}
      </ul>
    );
  };

  const mapWithTimestamp = (data: ChartRow[]) =>
    data.map(d => ({ ...d, timestamp: new Date(String(d.date)).getTime() }));

  const twrDataWithTs = useMemo(() => mapWithTimestamp(mergedChartData), [mergedChartData]);
  const mwrDataWithTs = useMemo(() => mapWithTimestamp(mwrChartData), [mwrChartData]);
  const drawdownDataWithTs = useMemo(() => mapWithTimestamp(drawdownChartData), [drawdownChartData]);
  const rollingDataWithTs = useMemo(() => mapWithTimestamp(rollingChartData), [rollingChartData]);

  const processedTwrData = useMemo(() => processAllCompareRanges(twrDataWithTs, 'Portfolio', availableCompareKeys, false), [twrDataWithTs, availableCompareKeys]);
  const processedRollingData = useMemo(() => processAllCompareRanges(rollingDataWithTs, 'Portfolio', availableCompareKeys, chartMode === 'rolling_volatility'), [rollingDataWithTs, availableCompareKeys, chartMode]);

  const getLineProps = (key: string) => ({
    onMouseEnter: () => availableCompareKeys.includes(key) && !hiddenSeries[key] ? setHoveredCompareKey(key) : null,
    onMouseLeave: () => availableCompareKeys.includes(key) ? setHoveredCompareKey(null) : null,
  });

  const renderGhostAreas = () => {
    if (activeCompareKey === null) return null;
    return (
      <>
        <Area hide={hiddenSeries[activeCompareKey]} type="monotone" dataKey={`Out_${activeCompareKey}`} fill="#34d399" fillOpacity={0.15} stroke="none" activeDot={false} isAnimationActive={false} tooltipType="none" legendType="none" />
        <Area hide={hiddenSeries[activeCompareKey]} type="monotone" dataKey={`Under_${activeCompareKey}`} fill="#f87171" fillOpacity={0.15} stroke="none" activeDot={false} isAnimationActive={false} tooltipType="none" legendType="none" />
      </>
    );
  };

  // formatter: recharts passes value as ValueType | undefined in this version
  const twrFormatter = (value: ValueType | undefined, name: NameType | undefined) => {
    if (String(name).startsWith('Out_') || String(name).startsWith('Under_')) return [];
    return [`${Number(value).toFixed(2)}%`, String(name)] as [string, string];
  };

  const rollingFormatter = (value: ValueType | undefined, name: NameType | undefined) => {
    if (String(name).startsWith('Out_') || String(name).startsWith('Under_')) return [];
    return [
      chartMode === 'rolling_volatility' ? `${(Number(value) * 100).toFixed(2)}%` : Number(value).toFixed(3),
      String(name)
    ] as [string, string];
  };

  if (chartMode === 'twr') {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={processedTwrData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
          <CartesianGrid {...COMMON_GRID_PROPS} />
          <XAxis {...COMMON_X_AXIS_PROPS} />
          <YAxis {...COMMON_Y_AXIS_PROPS} domain={['auto', 'auto']} tickFormatter={val => `${Number(val).toFixed(0)}%`} label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
          <Tooltip {...COMMON_TOOLTIP_PROPS} formatter={twrFormatter} />
          <Legend content={renderCustomLegend} />
          {renderGhostAreas()}

          <Line hide={hiddenSeries['Portfolio']} type="monotone" dataKey="Portfolio" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
          {compareLabel !== null && compareTwrHistory.length > 0 && (
            <Line {...getLineProps('Compare')} hide={hiddenSeries['Compare']} type="monotone" dataKey="Compare" name={compareLabel} stroke={COMPARE_COLOR} strokeWidth={2} strokeDasharray="4 2" dot={false} opacity={0.7} />
          )}
          {benchmarkSymbols.map((sym, i) => (
            <Line {...getLineProps(sym)} hide={hiddenSeries[sym]} key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
          ))}
          {scenarioBenchmarks.map((sb, i) => (
            <Line {...getLineProps(`[S] ${sb.name}`)} hide={hiddenSeries[`[S] ${sb.name}`]} key={`[S] ${sb.name}`} type="monotone" dataKey={`[S] ${sb.name}`} name={sb.name} stroke={COLORS[(benchmarkSymbols.length + i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="8 3" dot={false} opacity={0.85} />
          ))}
        </ComposedChart>
      </ResponsiveContainer>
    )
  }

  if (chartMode === 'mwr') {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={mwrDataWithTs} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
          <CartesianGrid {...COMMON_GRID_PROPS} />
          <XAxis {...COMMON_X_AXIS_PROPS} />
          <YAxis {...COMMON_Y_AXIS_PROPS} domain={['auto', 'auto']} tickFormatter={val => `${Number(val).toFixed(0)}%`} label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
          <Tooltip {...COMMON_TOOLTIP_PROPS} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)] as [string, string]} />
          <Legend content={renderCustomLegend} />

          <Line hide={hiddenSeries['Portfolio']} type="monotone" dataKey="Portfolio" name="Portfolio (MWR)" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
          {benchmarkSymbols.map((sym, i) => (
            <Line hide={hiddenSeries[sym]} key={sym} type="monotone" dataKey={sym} name={`${sym} (TWR)`} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
          ))}
        </LineChart>
      </ResponsiveContainer>
    )
  }

  if (chartMode === 'drawdown') {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={drawdownDataWithTs} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
          <CartesianGrid {...COMMON_GRID_PROPS} />
          <XAxis {...COMMON_X_AXIS_PROPS} />
          <YAxis {...COMMON_Y_AXIS_PROPS} domain={['auto', 0]} tickFormatter={val => `${Number(val).toFixed(0)}%`} label={{ value: 'Drawdown (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
          <Tooltip {...COMMON_TOOLTIP_PROPS} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)] as [string, string]} />
          {benchmarkSymbols.length > 0 && <Legend content={renderCustomLegend} />}

          <Area hide={hiddenSeries['Drawdown']} type="monotone" dataKey="Drawdown" stroke="#f87171" strokeWidth={1.5} fill="#f87171" fillOpacity={0.15} dot={false} animationDuration={1000} />
          {benchmarkSymbols.map((sym, i) => (
            <Line hide={hiddenSeries[sym]} key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    )
  }

  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart data={processedRollingData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
        <CartesianGrid {...COMMON_GRID_PROPS} />
        <XAxis {...COMMON_X_AXIS_PROPS} />
        <YAxis {...COMMON_Y_AXIS_PROPS} domain={['auto', 'auto']}
          tickFormatter={val => chartMode === 'rolling_volatility' ? `${(Number(val) * 100).toFixed(0)}%` : Number(val).toFixed(2)}
          label={{ value: rollingMetricLabel, angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }}
        />
        <Tooltip {...COMMON_TOOLTIP_PROPS} formatter={rollingFormatter} />
        <Legend content={renderCustomLegend} />
        {renderGhostAreas()}

        {Object.keys(rollingSeries).map((sym, i) => (
          <Line {...getLineProps(sym)} hide={hiddenSeries[sym]} key={sym} type="monotone" dataKey={sym} stroke={COLORS[i % COLORS.length]} strokeWidth={sym === 'Portfolio' ? 2.5 : 1.5} strokeDasharray={sym === 'Portfolio' ? undefined : '6 6'} dot={false} />
        ))}
      </ComposedChart>
    </ResponsiveContainer>
  )
}
