// Collect mono PCM off the renderer thread, in short transferable blocks.
class AtlasPCM extends AudioWorkletProcessor {
  constructor() {
    super();
    this.samples = new Float32Array(2048);
    this.used = 0;
    this.port.onmessage = () => {
      if (this.used) this.port.postMessage(this.samples.slice(0, this.used));
      this.used = 0;
      this.port.postMessage("flushed");
    };
  }
  process(inputs) {
    const input = inputs[0]?.[0];
    if (input) {
      for (const value of input) {
        this.samples[this.used++] = value;
        if (this.used === this.samples.length) {
          const block = this.samples;
          this.port.postMessage(block, [block.buffer]);
          this.samples = new Float32Array(2048);
          this.used = 0;
        }
      }
    }
    return true;
  }
}
registerProcessor("atlas-pcm", AtlasPCM);
