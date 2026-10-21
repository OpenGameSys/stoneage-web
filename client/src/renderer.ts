// WebGL2 精灵渲染：8 位页（R8UI 下标 + 调色板纹理查色）与 RGBA 页，连续使用同一纹理的图合成一次绘制。
// 加载约束见 h5/CLAUDE.md"资源格式规格" 10.5：必须关闭色彩空间转换与预乘。

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
    const gl = this.gl, W = gl.canvas.width, H = gl.canvas.height;
    gl.viewport(0, 0, W, H);
    gl.clearColor(clear[0], clear[1], clear[2], 1);
    gl.clear(gl.COLOR_BUFFER_BIT);
    this.drawCalls = 0;
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
