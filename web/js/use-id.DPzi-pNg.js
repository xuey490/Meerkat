import{C as e,Dt as t,O as n}from"./runtime-core.esm-bundler.DgOyhGNo.js";import{bt as r,gt as i,m as a,o}from"./style.Cy2L_Ar8.js";var s={prefix:Math.floor(Math.random()*1e4),current:0},c=Symbol(`elIdInjection`),l=()=>e()?n(c,s):s,u=e=>{let n=l();!r&&n===s&&a(`IdInjection`,`Looks like you are using server rendering, you must provide a id provider to ensure the hydration process to be succeed
usage: app.provide(ID_INJECTION_KEY, {
  prefix: number,
  current: number,
})`);let c=o();return i(()=>t(e)||`${c.value}-id-${n.prefix}-${n.current++}`)};export{l as n,u as t};