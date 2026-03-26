import { useEffect, useState } from 'react';
import NavBar from '../components/NavBar';
import { getTaxReport } from '../api';
import type { TaxReportResponse, TaxTransaction } from '../api';
import { escapeCSVField } from '../utils/format';

export default function TaxPage() {
  const [year, setYear] = useState<number>(new Date().getFullYear() - 1);
  const [report, setReport] = useState<TaxReportResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    async function fetchReport() {
      setLoading(true);
      setError('');
      try {
        const data = await getTaxReport(year);
        setReport(data);
      } catch (err: unknown) {
        if (err instanceof Error) {
          setError(err.message);
        } else {
          setError(String(err));
        }
      } finally {
        setLoading(false);
      }
    }
    fetchReport();
  }, [year]);

  const exportToCSV = (transactions: TaxTransaction[], filename: string, isInvestment: boolean) => {
    if (!transactions || transactions.length === 0) return;

    let headers, rows;
    if (isInvestment) {
      headers = [
        'Symbol', 'Sale Date', 'Buy Date', 'Quantity', 'Sell Price', 'Currency',
        'Buy Rate', 'Sell Rate', 'Cost CZK', 'Benefit CZK', 'Net Profit CZK'
      ];
      rows = transactions.map(t => [
        t.symbol,
        t.date,
        t.buy_date || '',
        t.quantity.toString(),
        t.native_price.toFixed(4),
        t.currency,
        t.buy_rate ? t.buy_rate.toFixed(4) : '',
        t.exchange_rate.toFixed(4),
        t.cost_czk.toFixed(2),
        t.benefit_czk.toFixed(2),
        (t.benefit_czk - t.cost_czk).toFixed(2)
      ]);
    } else {
      headers = [
        'Type', 'Symbol', 'Date', 'Quantity', 'Native Price', 'Currency',
        'Exchange Rate', 'Cost CZK', 'Benefit CZK', 'Net Profit CZK'
      ];
      rows = transactions.map(t => [
        t.type,
        t.symbol,
        t.date,
        t.quantity.toString(),
        t.native_price.toFixed(4),
        t.currency,
        t.exchange_rate.toFixed(4),
        t.cost_czk.toFixed(2),
        t.benefit_czk.toFixed(2),
        (t.benefit_czk - t.cost_czk).toFixed(2)
      ]);
    }

    const csvContent = [
      headers.map(escapeCSVField).join(','),
      ...rows.map(e => e.map(escapeCSVField).join(','))
    ].join('\n');

    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', filename);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const years = Array.from({ length: 10 }, (_, i) => new Date().getFullYear() - i);

  return (
    <div className="min-h-screen bg-[#0f1117] flex flex-col text-slate-200 selection:bg-indigo-500/30 font-sans tracking-tight">
      <NavBar />
      <main className="flex-1 flex items-center justify-center py-12 animate-fade-in">
        <div className="max-w-7xl w-full px-6">
        <div className="flex flex-col sm:flex-row items-baseline justify-between mb-8">
          <div>
            <h1 className="text-3xl font-bold tracking-tight text-white mb-2">Tax Calculation</h1>
            <p className="text-slate-400">View your Czech Republic tax figures for specific years.</p>
          </div>
          <div className="mt-4 sm:mt-0 flex items-center space-x-4">
            <label htmlFor="year-select" className="text-sm font-medium text-slate-300">
              Tax Year:
            </label>
            <select
              id="year-select"
              value={year}
              onChange={(e) => setYear(Number(e.target.value))}
              className="block w-full pl-3 pr-10 py-2 text-base border-gray-600 bg-slate-800 text-white focus:outline-none focus:ring-blue-500 focus:border-blue-500 sm:text-sm rounded-md shadow-sm"
            >
              {years.map(y => (
                <option key={y} value={y}>{y}</option>
              ))}
            </select>
          </div>
        </div>

        {loading ? (
          <div className="animate-pulse flex space-x-4">
            <div className="h-4 bg-slate-700 rounded w-1/4"></div>
            <div className="h-4 bg-slate-700 rounded w-3/4"></div>
          </div>
        ) : error ? (
          <div className="bg-red-900/50 border border-red-500 text-red-200 px-4 py-3 rounded relative" role="alert">
            <strong className="font-bold">Error loading tax report: </strong>
            <span className="block sm:inline">{error}</span>
          </div>
        ) : report ? (
          <div className="flex flex-col gap-12">
            {/* Section 1: Employment Income */}
            <div className="bg-slate-800 shadow rounded-xl overflow-hidden border border-slate-700">
              <div className="px-4 py-5 sm:px-6 flex justify-between items-center border-b border-slate-700 bg-slate-900/50">
                <div>
                  <h3 className="text-lg leading-6 font-medium text-white">Employment Income (Stock Plans)</h3>
                  <p className="mt-1 max-w-2xl text-sm text-slate-400">Section 6 - ESPP and RSU Vests</p>
                </div>
                <button
                  onClick={() => exportToCSV(report.employment_income.transactions, `employment_income_${year}.csv`, false)}
                  disabled={!report.employment_income.transactions?.length}
                className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                title="Export Employment Income to CSV (Excel Format)"
              >
                Export to CSV
              </button>
            </div>
            <div className="px-4 py-5 sm:p-6 text-slate-300">
              <dl className="grid grid-cols-1 gap-x-4 gap-y-8 sm:grid-cols-2 lg:grid-cols-3 bg-slate-900/30 p-4 rounded-md mb-6 border border-slate-700 pb-2 shadow-inner">
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Total Cost</dt>
                  <dd className="mt-1 text-2xl font-semibold text-rose-400">{report.employment_income.total_cost_czk.toLocaleString('cs-CZ')} CZK</dd>
                </div>
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Total Benefit</dt>
                  <dd className="mt-1 text-2xl font-semibold text-emerald-400">{report.employment_income.total_benefit_czk.toLocaleString('cs-CZ')} CZK</dd>
                </div>
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Net Profit</dt>
                  <dd className="mt-1 text-2xl font-semibold text-white">{(report.employment_income.total_benefit_czk - report.employment_income.total_cost_czk).toLocaleString('cs-CZ')} CZK</dd>
                </div>
              </dl>
              
              <div className="overflow-x-auto shadow ring-1 ring-black ring-opacity-5 md:rounded-lg">
                <table className="min-w-full divide-y divide-slate-700">
                  <thead className="bg-slate-800">
                    <tr>
                      <th scope="col" className="py-3.5 pl-4 pr-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider sm:pl-6">Type</th>
                      <th scope="col" className="px-3 py-3.5 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Symbol</th>
                      <th scope="col" className="px-3 py-3.5 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Date</th>
                      <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Qty</th>
                        <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Price (Native)</th>
                        <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">CNB Rate</th>
                        <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Cost (CZK)</th>
                        <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Benefit (CZK)</th>
                        <th scope="col" className="px-3 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">P/L (CZK)</th>
                      </tr>
                    </thead>
                  <tbody className="bg-slate-900 divide-y divide-slate-800">
                    {report.employment_income.transactions && report.employment_income.transactions.length > 0 ? (
                      report.employment_income.transactions.map((tx, idx) => (
                        <tr key={idx} className="hover:bg-slate-800/50 transition-colors">
                          <td className="whitespace-nowrap py-4 pl-4 pr-3 text-sm flex items-center gap-2 font-medium text-white sm:pl-6">
                            <span className={`w-2 h-2 rounded-full ${tx.type === 'ESPP_VEST' ? 'bg-indigo-400' : 'bg-fuchsia-400'}`}></span>
                            {tx.type}
                          </td>
                          <td className="whitespace-nowrap px-3 py-4 text-sm text-slate-300 font-semibold">{tx.symbol}</td>
                          <td className="whitespace-nowrap px-3 py-4 text-sm text-slate-400">{tx.date}</td>
                          <td className="whitespace-nowrap px-3 py-4 text-sm text-slate-300 text-right">{tx.quantity}</td>
                            <td className="whitespace-nowrap px-3 py-4 text-sm text-slate-300 text-right">{tx.native_price.toFixed(2)} {tx.currency}</td>
                            <td className="whitespace-nowrap px-3 py-4 text-sm text-slate-400 text-right font-mono text-xs">{tx.exchange_rate.toFixed(4)}</td>
                            <td className="whitespace-nowrap px-3 py-4 text-sm text-rose-300 text-right">{tx.cost_czk.toLocaleString('cs-CZ', {maximumFractionDigits: 2})}</td>
                            <td className="whitespace-nowrap px-3 py-4 text-sm text-emerald-300 text-bold text-right">{tx.benefit_czk.toLocaleString('cs-CZ', {maximumFractionDigits: 2})}</td>
                            <td className={`whitespace-nowrap px-3 py-4 text-sm font-semibold text-right ${(tx.benefit_czk - tx.cost_czk) >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                              {(tx.benefit_czk - tx.cost_czk) > 0 ? '+' : ''}{(tx.benefit_czk - tx.cost_czk).toLocaleString('cs-CZ', {maximumFractionDigits: 2})}
                            </td>
                          </tr>
                        ))
                    ) : (
                      <tr>
                        <td colSpan={8} className="whitespace-nowrap py-10 text-center text-sm text-slate-500">
                          No employment income transactions found for {year}.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          {/* Section 2: Investment Income */}
          <div className="bg-slate-800 shadow rounded-xl overflow-hidden border border-slate-700">
            <div className="px-4 py-5 sm:px-6 flex justify-between items-center border-b border-slate-700 bg-slate-900/50">
              <div>
                <h3 className="text-lg leading-6 font-medium text-white">Investment Income (Sales)</h3>
                <p className="mt-1 max-w-2xl text-sm text-slate-400">Section 10 - Paired via FIFO</p>
              </div>
              <button
                onClick={() => exportToCSV(report.investment_income.transactions, `investment_income_${year}.csv`, true)}
                disabled={!report.investment_income.transactions?.length}
                className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                title="Export Investment Income to CSV (Excel Format)"
              >
                Export to CSV
              </button>
            </div>
            <div className="px-4 py-5 sm:p-6 text-slate-300">
              <dl className="grid grid-cols-1 gap-x-4 gap-y-8 sm:grid-cols-2 lg:grid-cols-3 bg-slate-900/30 p-4 rounded-md mb-6 border border-slate-700 pb-2 shadow-inner">
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Total Cost</dt>
                  <dd className="mt-1 text-2xl font-semibold text-rose-400">{report.investment_income.total_cost_czk.toLocaleString('cs-CZ')} CZK</dd>
                </div>
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Total Benefit</dt>
                  <dd className="mt-1 text-2xl font-semibold text-emerald-400">{report.investment_income.total_benefit_czk.toLocaleString('cs-CZ')} CZK</dd>
                </div>
                <div className="sm:col-span-1">
                  <dt className="text-sm font-medium text-slate-400">Net Profit</dt>
                  <dd className="mt-1 text-2xl font-semibold text-white">{(report.investment_income.total_benefit_czk - report.investment_income.total_cost_czk).toLocaleString('cs-CZ')} CZK</dd>
                </div>
              </dl>
              
              <div className="overflow-x-auto shadow ring-1 ring-black ring-opacity-5 md:rounded-lg">
                <table className="min-w-full divide-y divide-slate-700">
                  <thead className="bg-slate-800">
                    <tr>
                      <th scope="col" className="py-3.5 pl-4 pr-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider sm:pl-6">Symbol</th>
                      <th scope="col" className="px-2 py-3.5 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Sale Date</th>
                      <th scope="col" className="px-2 py-3.5 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Paired Buy</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Qty</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Sell Price</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Rates</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Cost (CZK)</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Benefit (CZK)</th>
                      <th scope="col" className="px-2 py-3.5 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">P/L (CZK)</th>
                    </tr>
                  </thead>
                  <tbody className="bg-slate-900 divide-y divide-slate-800">
                    {report.investment_income.transactions && report.investment_income.transactions.length > 0 ? (
                      report.investment_income.transactions.map((tx, idx) => (
                        <tr key={idx} className="hover:bg-slate-800/50 transition-colors">
                          <td className="whitespace-nowrap py-3 pl-4 pr-3 text-sm font-bold text-white sm:pl-6">{tx.symbol}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-slate-300">{tx.date}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-slate-400">{tx.buy_date}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-slate-300 text-right">{tx.quantity.toLocaleString('cs-CZ')}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-slate-300 text-right">{tx.native_price.toFixed(2)} {tx.currency}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-xs font-mono text-slate-500 text-right leading-tight">
                            Buy: {tx.buy_rate?.toFixed(3)} <br/>
                            Sell: {tx.exchange_rate.toFixed(3)}
                          </td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-rose-300 text-right">{tx.cost_czk.toLocaleString('cs-CZ', {maximumFractionDigits: 2})}</td>
                          <td className="whitespace-nowrap px-2 py-3 text-sm text-emerald-300 text-right">{tx.benefit_czk.toLocaleString('cs-CZ', {maximumFractionDigits: 2})}</td>
                          <td className={`whitespace-nowrap px-2 py-3 text-sm font-semibold text-right ${(tx.benefit_czk - tx.cost_czk) >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                            {(tx.benefit_czk - tx.cost_czk) > 0 ? '+' : ''}{(tx.benefit_czk - tx.cost_czk).toLocaleString('cs-CZ', {maximumFractionDigits: 2})}
                          </td>
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={9} className="whitespace-nowrap py-10 text-center text-sm text-slate-500">
                          No investment income (sales) found for {year}.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      ) : null}
        </div>
      </main>
    </div>
);
}
