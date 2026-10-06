function(event) {
  const key = Symbol.for('atlas.agent.activity.v1');
  let state = window[key];
  if (!state) {
    const host = document.createElement('div');
    host.setAttribute('data-atlas-activity', '');
    host.setAttribute('aria-hidden', 'true');
    host.style.cssText = 'all:initial!important;position:fixed!important;inset:0!important;z-index:2147483647!important;pointer-events:none!important;contain:strict!important;';
    const nativeCursorStyle = document.createElement('style');
    nativeCursorStyle.textContent = 'html,html *{cursor:none!important}';
    host.append(nativeCursorStyle);
    const root = host.attachShadow({mode:'closed'});
    root.innerHTML = `<style>
      :host,*{pointer-events:none!important;user-select:none!important;box-sizing:border-box}
      .cursor{position:absolute;width:112px;height:112px;transform:translate(-48px,-48px)}
      .cursor .cursor-body{transform-box:view-box;transform-origin:48px 48px;transform:scale(var(--pointer-scale,1))}
      .cursor[data-held="true"] .cursor-body{fill:#435661;stroke:#f4f8fb;stroke-width:1.7}
      .edges{position:absolute;inset:0;isolation:isolate}
      .edges svg{display:block;width:100%;height:100%}
      .control-banner{position:absolute;top:20px;left:50%;transform:translateX(-50%);max-width:calc(100% - 32px);height:38px;display:flex;align-items:center;gap:12px;padding:0 16px 0 5px;border:1px solid transparent;border-radius:6px;background:linear-gradient(#171920,#171920) padding-box,linear-gradient(100deg,#ff5a81,#ffc452,#80ff9a,#4aefff,#7a93ff,#e76fff) border-box;color:#ebf4fa;font:600 13px/18px "Segoe UI",system-ui,sans-serif;white-space:nowrap;box-shadow:0 3px 12px #0003}
      .banner-icon{position:relative;display:block;flex:none;width:36px;height:36px;margin-right:-8px}
      .banner-icon svg,.banner-icon canvas{position:absolute;inset:0;display:block;width:100%;height:100%}
      .banner-icon [hidden]{display:none}
      .caption{min-width:0;overflow:hidden;text-overflow:ellipsis}
      .timer{flex:none;min-width:44px;text-align:right;font-weight:500;font-variant-numeric:tabular-nums;opacity:.6}
      .timer:empty{display:none}
      .waiting-hint{flex:none;font-weight:500;opacity:.72;padding-left:12px;border-left:1px solid #ffffff24}
      .waiting-hint[hidden]{display:none}
      .control-banner.waiting .caption::before{content:"";display:inline-block;width:7px;height:7px;margin:0 8px 1px 0;border-radius:50%;background:#ffc452;vertical-align:middle}
      .stop-hint{display:flex;flex:none;align-items:center;gap:10px;padding-left:12px;border-left:1px solid #ffffff24;font-weight:500}
      .stop-hint[hidden]{display:none}
      .stop-hint.inactive{opacity:.4}
      kbd{display:flex;flex:none;align-items:center;justify-content:center;width:30px;height:22px;font:600 11px/16px "Segoe UI",system-ui,sans-serif;border:1px solid #ffffff35;border-radius:4px;background:#ffffff0b}
    </style><div class="edges"></div><div class="control-banner"><span class="banner-icon"><span class="mascot-static">${event.banner_icon}</span><canvas class="mascot-live" hidden></canvas></span><span class="caption"></span><span class="timer"></span><span class="waiting-hint" hidden></span><span class="stop-hint"><kbd>Esc</kbd><span class="stop-label"></span></span></div><div class="cursor">${event.cursor}</div>`;
    const cursor=root.querySelector('.cursor');
    const edges=root.querySelector('.edges');
    const banner=root.querySelector('.control-banner');
    const mascotStatic=root.querySelector('.mascot-static'),mascotCanvas=root.querySelector('.mascot-live');
    const motion=matchMedia('(prefers-reduced-motion: reduce)');
    function resizeEdges(){
      const w=Math.max(1,innerWidth),h=Math.max(1,innerHeight),d=Math.min(112,w/2,h/2);
      const radius=Math.min(18,w/2,h/2),stops=[];
      for(let i=0;i<=d;i+=2){
        const alpha=0.44*Math.exp(-Math.pow(i/46,1.35))*Math.pow(Math.max(0,1-i/112),1.25);
        stops.push(`<stop offset="${i/d}" stop-color="white" stop-opacity="${alpha}"/>`);
      }
      stops.push('<stop offset="1" stop-color="white" stop-opacity="0"/>');
      const axes=[['t',0,0,0,d],['r',w,0,w-d,0],['b',0,h,0,h-d],['l',0,0,d,0]];
      const gradients=axes.map(([id,x1,y1,x2,y2])=>`<linearGradient id="atlas-fog-${id}" gradientUnits="userSpaceOnUse" x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}">${stops.join('')}</linearGradient>`).join('');
      const haze=axes.map(([id])=>`<rect class="feather" width="${w}" height="${h}" fill="url(#atlas-fog-${id})"/>`).join('');
      function cloudPath(inset,amplitude,shift){
        inset=Math.min(inset,w/4,h/4);const paths=[];
        const perimeter=2*(w+h);
        for(let side=0;side<4;side++){
          const points=[];
          for(let i=0;i<=48;i++){
            const t=i/48;let x,y,u;
            if(side===0){x=-d+(w+2*d)*t;u=x/perimeter;y=inset+amplitude*Math.sin((u*6+shift)*Math.PI*2)}
            else if(side===1){y=-d+(h+2*d)*t;u=(w+y)/perimeter;x=w-inset-amplitude*Math.sin((u*6+shift)*Math.PI*2)}
            else if(side===2){x=w+d-(w+2*d)*t;u=(w+h+w-x)/perimeter;y=h-inset-amplitude*Math.sin((u*6+shift)*Math.PI*2)}
            else{y=h+d-(h+2*d)*t;u=(2*w+h+h-y)/perimeter;x=inset+amplitude*Math.sin((u*6+shift)*Math.PI*2)}
            points.push(`${i?'L':'M'}${x.toFixed(2)} ${y.toFixed(2)}`);
          }
          paths.push(`<path d="${points.join('')}"/>`);
        }
        return paths.join('');
      }
      edges.innerHTML=`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><defs>${gradients}<clipPath id="atlas-frame-clip"><rect width="${w}" height="${h}" rx="${radius}"/></clipPath><filter id="atlas-smoke-soft" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="7"/></filter><mask id="atlas-edge-feather" maskUnits="userSpaceOnUse" x="0" y="0" width="${w}" height="${h}"><g clip-path="url(#atlas-frame-clip)">${haze}<rect class="rounded-frame" x=".5" y=".5" width="${Math.max(0,w-1)}" height="${Math.max(0,h-1)}" rx="${Math.max(0,radius-.5)}" fill="none" stroke="white" stroke-width="1.4" opacity=".88"/><g fill="none" stroke="white" filter="url(#atlas-smoke-soft)"><g class="smoke-band" stroke-width="28" opacity=".44">${cloudPath(26,10,0)}</g><g class="smoke-band" stroke-width="18" opacity=".30">${cloudPath(48,13,0.37)}</g></g></g></mask></defs><foreignObject x="0" y="0" width="${w}" height="${h}" mask="url(#atlas-edge-feather)"><div xmlns="http://www.w3.org/1999/xhtml" style="width:100%;height:100%;background:conic-gradient(from -135deg,#ff5a81,#ffc452,#80ff9a,#4aefff,#7a93ff,#e76fff,#ff5a81)"></div></foreignObject></svg>`;
      if(state)state.bands=edges.querySelectorAll('.smoke-band');
    }
    resizeEdges();
    window.addEventListener('resize',resizeEdges);
    state={host,edges,motion,bands:edges.querySelectorAll('.smoke-band'),id:0,timer:0,visible:false,x:innerWidth-48,y:innerHeight-48,
      displayX:innerWidth-48,displayY:innerHeight-48,fromX:0,fromY:0,moveAt:0,pointerReady:false,pointerKind:'',pointerRevision:0,cueAt:0,cueToken:'',cuePending:false,
      frame:0,frames:0,last:0,epoch:performance.now(),reduced:false,
      drawPointer(time,painted=false){
        const reduced=this.reduced||motion.matches;
        // Begin confirmed feedback on its first paint, even if delivery was late.
        if(this.cuePending&&painted){this.cueAt=time;this.cuePending=false}
        const t=reduced?1:Math.min(1,Math.max(0,(time-this.moveAt)/180)),ease=1-Math.pow(1-t,3);
        this.displayX=this.fromX+(this.x-this.fromX)*ease;this.displayY=this.fromY+(this.y-this.fromY)*ease;
        const held=['press','drag','drag_move'].includes(this.pointerKind);
        const age=time-this.cueAt;
        const press=this.pointerKind==='click'&&!this.cuePending&&age>=0&&age<260?1-age/260:0;
        cursor.style.left=this.displayX+'px';cursor.style.top=this.displayY+'px';
        cursor.dataset.held=String(held);
        cursor.style.setProperty('--pointer-scale',reduced?'1':String(held?.97:1-.14*press));
        cursor.querySelector('.cursor-click-ring').setAttribute('opacity',reduced?'0':String(.75*press));
      },
      animate(time){this.frame=0;if(!this.visible||!host.isConnected)return;
        this.drawPointer(time,true);
        if(this.reduced||motion.matches){edges.style.filter='none';edges.style.opacity='1';
          this.bands.forEach((band,i)=>{band.removeAttribute('transform');band.setAttribute('opacity',i===0?'.44':'.30')});return}
        const interval=1000/60;
        const delta=time-this.last;
        if(delta>=interval-0.5){this.last+=Math.max(1,Math.floor((delta+0.5)/interval))*interval;
          const phase=((time-this.epoch)%6000)/6000;
          edges.style.filter=`hue-rotate(${phase*360}deg)`;
          edges.style.opacity='1';
          this.bands.forEach((band,i)=>{const angle=(phase+i*0.37)*Math.PI*2;
            band.setAttribute('transform',`translate(${Math.sin(angle)*7} ${Math.cos(angle)*5})`);
            band.setAttribute('opacity',(i===0?0.44:0.30)+0.10*Math.sin(angle));});this.frames++}
        this.frame=requestAnimationFrame(t=>this.animate(t));
      },
      start(){if(!this.frame)this.frame=requestAnimationFrame(t=>this.animate(t))},
      remove(){cancelAnimationFrame(this.frame);this.frame=0;clearInterval(this.clock);this.clock=0;host.remove();mascotCanvas.hidden=true;mascotStatic.hidden=false},
      // The run timer counts from Atlas's monotonic elapsed time.
      tick(){const t=banner.querySelector('.timer');if(this.elapsedAt===undefined){t.textContent='';return}
        const ms=this.elapsed+(this.running?performance.now()-this.elapsedAt:0),s=Math.max(0,Math.floor(ms/1000)),h=Math.floor(s/3600),m=Math.floor(s/60)%60,p=n=>String(n).padStart(2,'0');
        const text=h?`${h}:${p(m)}:${p(s%60)}`:`${p(m)}:${p(s%60)}`;if(t.textContent!==text)t.textContent=text},
      // Paints a frame rendered by Atlas's Go character engine.
      mascot(w,h,data){
        if(!this.visible)return null;
        const ctx=this.mascotContext||(this.mascotContext=mascotCanvas.getContext&&mascotCanvas.getContext('2d'));
        if(!ctx)return null;
        if(mascotCanvas.width!==w||mascotCanvas.height!==h){mascotCanvas.width=w;mascotCanvas.height=h}
        const bytes=atob(data),image=ctx.createImageData(w,h),px=image.data;
        if(bytes.length!==px.length)return null;
        for(let i=0;i<px.length;i++)px[i]=bytes.charCodeAt(i);
        ctx.putImageData(image,0,0);
        if(mascotCanvas.hidden){mascotCanvas.hidden=false;mascotStatic.hidden=true}
        return [devicePixelRatio||1,innerWidth,innerHeight];
      },
      restore(){if(this.visible){if(!host.isConnected)document.documentElement.append(host);this.start()}},
      update(e){const previousID=this.id;this.id=e.id;this.visible=e.visible;clearTimeout(this.timer);if(!e.visible){this.pointerReady=false;this.remove();return}
        this.canStop=!!e.can_stop;banner.querySelector('.caption').textContent=e.caption;
        banner.classList.toggle('waiting',!!e.waiting);
        const waitingHint=banner.querySelector('.waiting-hint');waitingHint.textContent=e.waiting?e.waiting_hint||'':'';waitingHint.hidden=!e.waiting;
        this.elapsed=e.elapsed||0;this.running=!!e.running;this.elapsedAt=e.elapsed>0||e.running?performance.now():undefined;this.tick();
        if(!this.clock)this.clock=setInterval(()=>this.tick(),1000);
        // A completed run releases control at once; only the banner settles,
        // keeping its stop area in place but inactive.
        const done=!!e.done,hint=banner.querySelector('.stop-hint');
        banner.querySelector('.stop-label').textContent=e.stop;hint.hidden=!this.canStop&&!(done&&!hint.hidden);
        hint.classList.toggle('inactive',!this.canStop);
        cursor.style.display=done?'none':'';edges.style.display=done?'none':'';nativeCursorStyle.disabled=done;
        this.reduced=!!e.reduced;
        const now=performance.now();
        if(!this.pointerReady){this.fromX=this.x;this.fromY=this.y;this.moveAt=now-180}
        if(previousID!==e.id||this.pointerRevision!==e.pointer_revision||this.pointerKind!==e.pointer_kind){
          this.pointerKind=e.pointer_kind||'';this.pointerRevision=e.pointer_revision||0;
          const token=e.pointer_stamp||e.id+':'+e.pointer_revision;
          if(this.pointerKind==='click'){
            if(this.cueToken!==token){this.cueToken=token;this.cuePending=!(this.reduced||motion.matches);this.cueAt=now}
          }else{this.cuePending=false;this.cueAt=now-(e.pointer_age||0)}
        }
        if(e.Point){
          const snap=this.reduced||motion.matches||!['','move','drag_move','aim'].includes(this.pointerKind);
          if(e.X!==this.x||e.Y!==this.y||snap){
            this.drawPointer(now);this.fromX=snap?e.X:this.displayX;this.fromY=snap?e.Y:this.displayY;
            this.x=e.X;this.y=e.Y;this.moveAt=snap?now-180:now;
          }
        }else if(!this.pointerReady){this.fromX=this.x;this.fromY=this.y;this.moveAt=now-180}
        this.pointerReady=true;this.drawPointer(now);
        this.restore();this.timer=setTimeout(()=>{this.visible=false;this.remove()},e.duration>0?e.duration:30000);
      }};
    motion.addEventListener('change',()=>{if(state.visible)state.start()});
    Object.defineProperty(window,key,{value:state,configurable:true});
  }
    state.update(event);
    if(event.pointer_kind==='aim'&&!state.reduced&&!state.motion.matches){
      const owner=state.id,deadline=performance.now()+400;
      return new Promise(resolve=>{
        const frame=()=>{
          if(state.id!==owner||!state.visible){resolve(false);return}
          if(performance.now()>=deadline||Math.hypot(state.displayX-event.X,state.displayY-event.Y)<.1){
            state.fromX=state.x=event.X;state.fromY=state.y=event.Y;state.moveAt=performance.now()-180;state.drawPointer(performance.now());resolve(true);return
          }
          requestAnimationFrame(frame);
        };
        requestAnimationFrame(frame);
      });
    }
}
