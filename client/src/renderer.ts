// WebGL2 精灵渲染：8 位页（R8UI 下标 + 调色板纹理查色）与 RGBA 页，连续使用同一纹理的图合成一次绘制。
// 加载约束见 h5/CLAUDE.md"资源格式规格" 10.5：必须关闭色彩空间转换与预乘。
// 清晰度：场景永远按逻辑分辨率 800×600 画进离屏纹理（像素画一个像素对一个纹素），再按画布的实际像素数（CSS 大小 × devicePixelRatio，
// main.ts 的 fitView）用"锐利的双线性"放大到画布：像素内部不插值、只在相邻像素的交界处用一个屏幕像素过渡，
// 非整数倍放大时既不糊也没有宽窄不一的像素；缩小时退化成双线性（近似面积平均），比浏览器把 800×600 的画布拉伸要清楚。

export interface Texture { tex: WebGLTexture; indexed: boolean; w: number; h: number; }
export interface Quad { tex: Texture; x: number; y: number; u: number; v: number; w: number; h: number; }

const VS = `#version 300 es
in vec2 aPos; in vec2 aUv; out vec2 vUv; uniform vec2 uView;
void main(){ vUv = aUv; gl_Position = vec4(aPos / uView * vec2(2.,-2.) + vec2(-1.,1.), 0., 1.); }`;
const FS_I8 = `#version 300 es
precision highp float; in vec2 vUv; out vec4 o;
uniform highp usampler2D uPage; uniform sampler2D uPal; uniform int uRowA, uRowB; uniform float uMix;
void main(){
  uint idx = texelFetch(uPage, ivec2(vUv), 0).r;
  if (idx == 0u) discard;
  o = mix(texelFetch(uPal, ivec2(int(idx), uRowA), 0), texelFetch(uPal, ivec2(int(idx), uRowB), 0), uMix);
}`;
const FS_RGBA = `#version 300 es
precision highp float; in vec2 vUv; out vec4 o; uniform sampler2D uPage;
void main(){ vec4 c = texelFetch(uPage, ivec2(vUv), 0); if (c.a == 0.) discard; o = c; }`;

const SCENE_W = 800, SCENE_H = 600; // 逻辑分辨率（界面座标）
const VS_BLIT = `#version 300 es
in vec2 aPos; out vec2 vUv;
void main(){ vUv = aPos * 0.5 + 0.5; gl_Position = vec4(aPos, 0., 1.); }`;
// 场景纹理在 FBO 里 v=0 是逻辑座标的下缘（场景着色器把 y 翻过来画），所以 vUv 不必再翻
const FS_BLIT = `#version 300 es
precision highp float; in vec2 vUv; out vec4 o; uniform sampler2D uTex; uniform vec2 uSize;
void main(){
  vec2 p = vUv * uSize;
  vec2 seam = floor(p + 0.5);
  vec2 d = max(fwidth(p), vec2(1e-5));
  p = seam + clamp((p - seam) / min(d, vec2(1.)), -0.5, 0.5); // 缩小（d > 1）时不改座标，交给 mipmap 的三线性过滤
  o = vec4(texture(uTex, p / uSize).rgb, 1.);
}`;

const FLOATS_PER_QUAD = 24; // 两个三角形 × 3 顶点 × (x, y, u, v)

interface Prog { p: WebGLProgram; vao: WebGLVertexArrayObject; uView: WebGLUniformLocation | null; uPage: WebGLUniformLocation | null;
  uPal: WebGLUniformLocation | null; uRowA: WebGLUniformLocation | null; uRowB: WebGLUniformLocation | null; uMix: WebGLUniformLocation | null; }

export class Renderer {
  readonly gl: WebGL2RenderingContext;
  private progI8: Prog;
  private progRgba: Prog;
  private vbo: WebGLBuffer;
  private verts = new Float32Array(4096 * FLOATS_PER_QUAD); // 不够时加倍
  private vboSize = 0;
  private palette: Texture | null = null;
  private fbo: WebGLFramebuffer;      // 800×600 的离屏场景
  private scene: WebGLTexture;
  private blit: { p: WebGLProgram; vao: WebGLVertexArrayObject; uTex: WebGLUniformLocation | null; uSize: WebGLUniformLocation | null };
  paletteA = 0;
  paletteB = 0;
  paletteMix = 0;
  drawCalls = 0;

  constructor(canvas: HTMLCanvasElement) {
    const gl = canvas.getContext("webgl2", { premultipliedAlpha: false, antialias: false });
    if (!gl) throw new Error("浏览器不支持 WebGL2");
    this.gl = gl;
    this.vbo = gl.createBuffer()!;
    this.progI8 = this.program(FS_I8);
    this.progRgba = this.program(FS_RGBA);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.NONE);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
    // 离屏场景：RGBA8、双线性（锐利放大的着色器靠它在交界处插值）、边缘夹住；WebGL2 的非 2 次幂纹理也能做 mipmap
    this.scene = gl.createTexture()!;
    gl.bindTexture(gl.TEXTURE_2D, this.scene);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, SCENE_W, SCENE_H, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR); // 缩小时 present 换成 mipmap 的三线性（不会丢像素）
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    this.fbo = gl.createFramebuffer()!;
    gl.bindFramebuffer(gl.FRAMEBUFFER, this.fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, this.scene, 0);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    this.blit = this.blitProgram();
  }

  /** 把离屏场景放大到画布的着色器：一个盖满画布的矩形（两个三角形） */
  private blitProgram() {
    const gl = this.gl, p = gl.createProgram()!;
    for (const [type, src] of [[gl.VERTEX_SHADER, VS_BLIT], [gl.FRAGMENT_SHADER, FS_BLIT]] as [number, string][]) {
      const s = gl.createShader(type)!;
      gl.shaderSource(s, src);
      gl.compileShader(s);
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? "shader");
      gl.attachShader(p, s);
    }
    gl.linkProgram(p);
    const vao = gl.createVertexArray()!, vbo = gl.createBuffer()!;
    gl.bindVertexArray(vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, -1, 1, 1, -1, 1]), gl.STATIC_DRAW);
    const aPos = gl.getAttribLocation(p, "aPos");
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 8, 0);
    gl.bindVertexArray(null);
    return { p, vao, uTex: gl.getUniformLocation(p, "uTex"), uSize: gl.getUniformLocation(p, "uSize") };
  }

  /** 编译着色器，查好 uniform 位置，建一个绑着顶点缓冲的 VAO（之后每次绘制不必再查、再设定顶点格式） */
  private program(fs: string): Prog {
    const gl = this.gl, p = gl.createProgram()!;
    for (const [type, src] of [[gl.VERTEX_SHADER, VS], [gl.FRAGMENT_SHADER, fs]] as [number, string][]) {
      const s = gl.createShader(type)!;
      gl.shaderSource(s, src);
      gl.compileShader(s);
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? "shader");
      gl.attachShader(p, s);
    }
    gl.linkProgram(p);
    const vao = gl.createVertexArray()!;
    gl.bindVertexArray(vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
    const aPos = gl.getAttribLocation(p, "aPos"), aUv = gl.getAttribLocation(p, "aUv");
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 16, 0);
    gl.enableVertexAttribArray(aUv);
    gl.vertexAttribPointer(aUv, 2, gl.FLOAT, false, 16, 8);
    gl.bindVertexArray(null);
    const u = (n: string) => gl.getUniformLocation(p, n);
    return { p, vao, uView: u("uView"), uPage: u("uPage"), uPal: u("uPal"), uRowA: u("uRowA"), uRowB: u("uRowB"), uMix: u("uMix") };
  }

  async loadTexture(url: string, indexed: boolean): Promise<Texture> {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${url}: ${res.status}`);
    const bmp = await createImageBitmap(await res.blob(), { colorSpaceConversion: "none", premultiplyAlpha: "none" });
    const gl = this.gl, tex = gl.createTexture()!;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    if (indexed) gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8UI, gl.RED_INTEGER, gl.UNSIGNED_BYTE, bmp);
    else gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, bmp);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    const t = { tex, indexed, w: bmp.width, h: bmp.height };
    bmp.close();
    return t;
  }

  setPalette(tex: Texture) { this.palette = tex; }

  /** 释放一张纹理的显存（Assets 的显存预算用） */
  deleteTexture(t: Texture) { this.gl.deleteTexture(t.tex); }

  /** 页面真的关掉时主动交还 WebGL 上下文与显存（浏览器本来也会回收，这只是让它早一点） */
  dispose() { this.gl.getExtension("WEBGL_lose_context")?.loseContext(); }

  /**
   * 按顺序绘制；相邻且纹理相同的图合并成一次绘制。整帧的顶点一次写进缓冲（只上传一次），
   * 每批只换纹理、画缓冲里的那一段。
   */
  draw(quads: Quad[], clear: [number, number, number]) {
    const gl = this.gl, W = SCENE_W, H = SCENE_H;
    gl.bindFramebuffer(gl.FRAMEBUFFER, this.fbo);
    gl.viewport(0, 0, W, H);
    gl.clearColor(clear[0], clear[1], clear[2], 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    this.drawCalls = 0;
    this.drawScene(quads, W, H);
    this.present();
  }

  /** 离屏场景放大到画布（画布大小由 main.ts 的 fitView 跟着显示大小设定） */
  private present() {
    const gl = this.gl;
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.viewport(0, 0, gl.canvas.width, gl.canvas.height);
    gl.disable(gl.BLEND);
    gl.useProgram(this.blit.p);
    gl.bindVertexArray(this.blit.vao);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.scene);
    const shrink = gl.canvas.width < SCENE_W || gl.canvas.height < SCENE_H;
    if (shrink) gl.generateMipmap(gl.TEXTURE_2D); // 只有缩小时才做；没有 mipmap 时不能用 mipmap 过滤（纹理不完整会采样成黑色）
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, shrink ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR);
    gl.uniform1i(this.blit.uTex, 0);
    gl.uniform2f(this.blit.uSize, SCENE_W, SCENE_H);
    gl.drawArrays(gl.TRIANGLES, 0, 6);
    gl.bindVertexArray(null);
    gl.enable(gl.BLEND);
  }

  private drawScene(quads: Quad[], W: number, H: number) {
    const gl = this.gl;
    if (quads.length === 0) return;
    if (quads.length * FLOATS_PER_QUAD > this.verts.length) {
      let n = this.verts.length;
      while (n < quads.length * FLOATS_PER_QUAD) n *= 2;
      this.verts = new Float32Array(n);
    }
    const v = this.verts;
    let o = 0;
    for (const q of quads) {
      const x0 = q.x, y0 = q.y, x1 = q.x + q.w, y1 = q.y + q.h, u0 = q.u, v0 = q.v, u1 = q.u + q.w, v1 = q.v + q.h;
      v[o] = x0; v[o + 1] = y0; v[o + 2] = u0; v[o + 3] = v0;
      v[o + 4] = x1; v[o + 5] = y0; v[o + 6] = u1; v[o + 7] = v0;
      v[o + 8] = x0; v[o + 9] = y1; v[o + 10] = u0; v[o + 11] = v1;
      v[o + 12] = x1; v[o + 13] = y0; v[o + 14] = u1; v[o + 15] = v0;
      v[o + 16] = x1; v[o + 17] = y1; v[o + 18] = u1; v[o + 19] = v1;
      v[o + 20] = x0; v[o + 21] = y1; v[o + 22] = u0; v[o + 23] = v1;
      o += FLOATS_PER_QUAD;
    }
    gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
    if (this.vboSize < v.length) { gl.bufferData(gl.ARRAY_BUFFER, v.byteLength, gl.STREAM_DRAW); this.vboSize = v.length; }
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, v, 0, o);
    let cur: Prog | null = null, start = 0;
    for (let i = 1; i <= quads.length; i++) {
      if (i < quads.length && quads[i].tex === quads[start].tex) continue;
      const tex = quads[start].tex, p = tex.indexed ? this.progI8 : this.progRgba;
      if (p !== cur) {
        cur = p;
        gl.useProgram(p.p);
        gl.bindVertexArray(p.vao);
        gl.uniform2f(p.uView, W, H);
        gl.uniform1i(p.uPage, 0);
        if (tex.indexed && this.palette) {
          gl.activeTexture(gl.TEXTURE1);
          gl.bindTexture(gl.TEXTURE_2D, this.palette.tex);
          gl.uniform1i(p.uPal, 1);
          gl.uniform1i(p.uRowA, this.paletteA);
          gl.uniform1i(p.uRowB, this.paletteB);
          gl.uniform1f(p.uMix, this.paletteMix);
        }
        gl.activeTexture(gl.TEXTURE0);
      }
      gl.bindTexture(gl.TEXTURE_2D, tex.tex);
      gl.drawArrays(gl.TRIANGLES, start * 6, (i - start) * 6);
      this.drawCalls++;
      start = i;
    }
    gl.bindVertexArray(null);
  }
}
