import{_ as s,o as n,c as a,S as l}from"./chunks/framework.662a2917.js";const A=JSON.parse('{"title":"inject","description":"","frontmatter":{},"headers":[],"relativePath":"reference/server/middlewares/inject.md","filePath":"reference/server/middlewares/inject.md"}'),p={name:"reference/server/middlewares/inject.md"},e=l(`<h1 id="inject" tabindex="-1">inject <a class="header-anchor" href="#inject" aria-label="Permalink to &quot;inject&quot;">​</a></h1><p>Inject middleware help to change content of the anything. Give a content-type you want to change it.</p><div class="language-yaml"><button title="Copy Code" class="copy"></button><span class="lang">yaml</span><pre class="shiki material-theme-palenight"><code><span class="line"><span style="color:#F07178;">middlewares</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">  </span><span style="color:#F07178;">test</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">    </span><span style="color:#F07178;">inject</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">path_map</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">/test</span><span style="color:#89DDFF;">&quot;</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#676E95;font-style:italic;"># checking with filepath.Match</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">regex</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;&quot;</span><span style="color:#A6ACCD;"> </span><span style="color:#676E95;font-style:italic;"># old is ignored if regex is set</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">old</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;&quot;</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">new</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;&quot;</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#F07178;">content_map</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#676E95;font-style:italic;"># map of content-type</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">text/html</span><span style="color:#89DDFF;">&quot;</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">          </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">regex</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;&quot;</span><span style="color:#A6ACCD;"> </span><span style="color:#676E95;font-style:italic;"># old is ignored if regex is set</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">old</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">my text</span><span style="color:#89DDFF;">&quot;</span></span>
<span class="line"><span style="color:#A6ACCD;">            </span><span style="color:#F07178;">new</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">my mext</span><span style="color:#89DDFF;">&quot;</span></span></code></pre></div><p>Example:</p><div class="language-yaml"><button title="Copy Code" class="copy"></button><span class="lang">yaml</span><pre class="shiki material-theme-palenight"><code><span class="line"><span style="color:#F07178;">inject</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">  </span><span style="color:#F07178;">content_map</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">    </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">text/html</span><span style="color:#89DDFF;">&quot;</span><span style="color:#89DDFF;">:</span></span>
<span class="line"><span style="color:#A6ACCD;">      </span><span style="color:#89DDFF;">-</span><span style="color:#A6ACCD;"> </span><span style="color:#F07178;">old</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;">&quot;</span><span style="color:#C3E88D;">&lt;/head&gt;</span><span style="color:#89DDFF;">&quot;</span></span>
<span class="line"><span style="color:#A6ACCD;">        </span><span style="color:#F07178;">new</span><span style="color:#89DDFF;">:</span><span style="color:#A6ACCD;"> </span><span style="color:#89DDFF;font-style:italic;">|</span></span>
<span class="line"><span style="color:#C3E88D;">          &lt;script defer&gt;</span></span>
<span class="line"><span style="color:#C3E88D;">          // Override the fetch function</span></span>
<span class="line"><span style="color:#C3E88D;">          window.fetch = async function(...args) {</span></span>
<span class="line"><span style="color:#C3E88D;">            try {</span></span>
<span class="line"><span style="color:#C3E88D;">            // Use the original fetch function and get the response</span></span>
<span class="line"><span style="color:#C3E88D;">            const response = await originalFetch(...args);</span></span>
<span class="line"></span>
<span class="line"><span style="color:#C3E88D;">            // Check for the 407 status code</span></span>
<span class="line"><span style="color:#C3E88D;">            if (response.status === 407) {</span></span>
<span class="line"><span style="color:#C3E88D;">              location.reload();  // Refresh the page</span></span>
<span class="line"><span style="color:#C3E88D;">              return;  // Optionally, you can throw an error or return a custom response here</span></span>
<span class="line"><span style="color:#C3E88D;">            }</span></span>
<span class="line"></span>
<span class="line"><span style="color:#C3E88D;">            // Return the original response for other cases</span></span>
<span class="line"><span style="color:#C3E88D;">            return response;</span></span>
<span class="line"><span style="color:#C3E88D;">            } catch (error) {</span></span>
<span class="line"><span style="color:#C3E88D;">            throw error;  // Rethrow any errors that occurred during the fetch</span></span>
<span class="line"><span style="color:#C3E88D;">            }</span></span>
<span class="line"><span style="color:#C3E88D;">          }</span></span>
<span class="line"><span style="color:#C3E88D;">          &lt;/script&gt;</span></span>
<span class="line"><span style="color:#C3E88D;">          &lt;/head&gt;</span></span></code></pre></div>`,5),o=[e];function t(c,r,y,D,i,C){return n(),a("div",null,o)}const d=s(p,[["render",t]]);export{A as __pageData,d as default};
