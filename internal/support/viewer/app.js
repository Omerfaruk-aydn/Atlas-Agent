"use strict";
const $=id=>document.getElementById(id);
let stamp=0,hidden=false,refreshing=false;
async function action(name){try{const r=await fetch(name,{method:"POST"});if(!r.ok)throw new Error("İstek kabul edilmedi.");await refresh()}catch(e){$("status").textContent=e.message}}
$("start").onclick=()=>action("start");$("stop").onclick=()=>action("stop");
async function refresh(){
 if(hidden||refreshing)return;
 refreshing=true;
 try{
  const r=await fetch("state",{cache:"no-store",signal:AbortSignal.timeout(5000)});if(!r.ok)throw new Error("Panel bağlantısı kapandı.");const s=await r.json();
  $("device").textContent=s.device||"Oturum değişti";$("start").disabled=s.watching;$("stop").disabled=!s.watching;
  $("status").textContent=s.error?"Ekran güncel değil: "+s.error:s.frame?(s.cached?"Son görüntü güncel olmayabilir. ":"Son görüntü: ")+new Date(s.captured_at).toLocaleTimeString("tr-TR"):"Henüz ekran yakalanmadı.";
  $("frame").hidden=!s.frame;$("empty").hidden=s.frame;
  if(s.frame&&stamp!==s.captured_at){stamp=s.captured_at;$("frame").src="frame?t="+stamp}
  $("timeline").replaceChildren(...(s.timeline||[]).map(e=>{const li=document.createElement("li"),small=document.createElement("small");li.textContent=e.detail||e.kind;small.textContent=[new Date(e.at).toLocaleTimeString("tr-TR"),e.state,e.effect,e.verification].filter(Boolean).join(" · ");li.append(small);return li}));
 }catch(e){$("status").textContent=e.message;$("frame").alt="Önbellekteki son görüntü; bağlantı kapandı."}finally{refreshing=false}
}
document.addEventListener("visibilitychange",()=>{hidden=document.hidden;if(!hidden)refresh()});
refresh();setInterval(refresh,1000);
