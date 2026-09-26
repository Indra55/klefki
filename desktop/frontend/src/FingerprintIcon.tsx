// Adapted from https://lucide-animated.com/icons/fingerprint; see fingerprint-license.txt.
const paths=[
 'M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4',
 'M14 13.12c0 2.38 0 6.38-1 8.88',
 'M17.29 21.02c.12-.6.43-2.3.5-3.02',
 'M2 12a10 10 0 0 1 18-6',
 'M2 16h.01',
 'M21.8 16c.2-2 .131-5.354 0-6',
 'M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2',
 'M8.65 22c.21-.66.45-1.32.57-2',
 'M9 6.8a6 6 0 0 1 9 5.2v2',
];
export type FingerprintAnimation='idle'|'scanning'|'retry'|'success'|'error';
export function FingerprintIcon({status='idle'}:{status?:FingerprintAnimation}){
 return <svg className="fingerprint-icon" data-state={status} width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
  <g className="fingerprint-outline">{paths.map(d=><path key={d} d={d}/>)}</g>
  <g className="fingerprint-ridges">{paths.map(d=><path key={d} d={d} pathLength="1"/>)}</g>
  {status==='success'&&<path className="fingerprint-check" d="m5 12 4 4 10-10" pathLength="1"/>}
 </svg>;
}
