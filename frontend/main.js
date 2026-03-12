/**
 * Shipper Frontend Logic
 * Handles project management, dashboard rendering, and build triggering.
 */

const API_BASE = './api';

// DOM Elements
const dom = {
    projectsContainer: document.getElementById('projects-container'),
    btnOpenModal: document.getElementById('btn-open-modal'),
    modal: document.getElementById('add-modal'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    btnCancelModal: document.getElementById('btn-cancel-modal'),
    form: document.getElementById('add-project-form'),
    inputs: {
        name: document.getElementById('proj-name'),
        repo: document.getElementById('proj-repo'),
        branch: document.getElementById('proj-branch'),
        registry: document.getElementById('proj-registry'),
    },
    // Global Modals
    backdrop: document.getElementById('modal-backdrop'),
    
    // Builds & Logs Modal
    buildsModal: document.getElementById('builds-modal'),
    buildList: document.getElementById('build-list-container'),
    logViewer: document.getElementById('log-viewer'),
    btnDeleteBuild: document.getElementById('btn-delete-build'),
    
    // Settings Modal
    settingsModal: document.getElementById('settings-modal'),
    editProjectId: document.getElementById('edit-project-id'),
    editTags: document.getElementById('edit-tags'),

    buildControls: document.getElementById('build-controls'),
    newBuildTag: document.getElementById('new-build-tag'),

    editRepo: document.getElementById('edit-repo'),
    editBranch: document.getElementById('edit-branch'),
    existingBuildTags: document.getElementById('existing-build-tags'),

    btnGlobalSettings: document.getElementById('btn-global-settings'),
    globalSettingsModal: document.getElementById('global-settings-modal'),
    pushTargetRegistry: document.getElementById('push-target-registry'),
    gs: {
        poll: document.getElementById('gs-poll'),
        retention: document.getElementById('gs-retention'),
        token: document.getElementById('gs-token'),
        registries: document.getElementById('gs-registries')
    }
};

// State
let currentProjectId;
let currentBuildId;
let globalProjects = [];
let logStreamInterval;

async function init() {
    bindEvents();
    await fetchGlobalSettings();
    fetchProjects();
    setupSSE();
}

function bindEvents() {
    // Modal controls
    dom.btnOpenModal.addEventListener('click', openModal);
    dom.btnCloseModal.addEventListener('click', closeModal);
    dom.btnCancelModal.addEventListener('click', closeModal);
    dom.btnGlobalSettings.addEventListener('click', openGlobalSettings);

    // Form submission
    dom.form.addEventListener('submit', handleAddProject);
    
    // Event delegation for project cards (Build buttons)
    dom.projectsContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-action]');
        if (!btn) return;

        const id = btn.dataset.id;
        const action = btn.dataset.action;

        if (action === 'build') triggerBuild(id);
        if (action === 'logs') openBuildsModal(id);
        if (action === 'settings') openSettingsModal(id);
    });

    if(dom.btnDeleteBuild) {
        dom.btnDeleteBuild.addEventListener('click', () => {
            if(currentBuildId) deleteBuild(currentBuildId);
        });
    }
}

function setupSSE() {
    const evtSource = new EventSource(`${API_BASE}/events`);
    
    evtSource.onmessage = (event) => {
        if (event.data === "update") {
            console.log("Backend triggered an update!");
            fetchProjects(); // Instantly refresh UI without waiting
        }
    };

    evtSource.onerror = () => {
        console.error("SSE connection lost. Reconnecting...");
        // EventSource auto-reconnects natively, no extra code needed!
    };
}

// --- UI Actions ---

function openModal() {
    dom.modal.classList.remove('hidden');
    dom.inputs.name.focus();
}

function closeModal() {
    dom.modal.classList.add('hidden');
    dom.form.reset();
}

window.closeModals = () => {
    clearInterval(logStreamInterval);
    if(dom.backdrop) dom.backdrop.classList.add('hidden');
    if(dom.buildsModal) dom.buildsModal.classList.add('hidden');
    if(dom.settingsModal) dom.settingsModal.classList.add('hidden');
    if(dom.globalSettingsModal) dom.globalSettingsModal.classList.add('hidden');
    currentProjectId = null;
    currentBuildId = null;
};

async function fetchGlobalSettings() {
    try {
        const res = await fetch(`${API_BASE}/settings`);
        if (!res.ok) return;
        const settings = await res.json();
        
        dom.gs.poll.value = settings.poll_interval || '1h';
        dom.gs.retention.value = settings.retention_policy || 'one_per_minor';
        dom.gs.token.value = settings.gh_token === '********' ? '********' : '';

        // Populate Registry Dropdowns & Settings List
        const regList = document.getElementById('registry-auth-list');
        regList.innerHTML = '';
        let options = '';

        if (settings.registries && settings.registries.length > 0) {
            settings.registries.forEach(reg => {
                options += `<option value="${reg.url}">${reg.url}</option>`;
                addRegistryField(reg.url, reg.username, reg.password);
            });
        }
        
        if(dom.inputs.registry) dom.inputs.registry.innerHTML = `<option value="">Default Registry</option>` + options;
        if(dom.pushTargetRegistry) dom.pushTargetRegistry.innerHTML = options;

    } catch (e) {
        console.error("Failed to load settings", e);
    }
}

// Settings and configuration
function openSettingsModal(projectId) {
    currentProjectId = projectId;
    const project = globalProjects.find(p => p.id == projectId);
    
    dom.editProjectId.value = projectId;
    dom.editRepo.value = project ? project.repo_url : '';
    dom.editBranch.value = project ? project.branch : '';    
    dom.editTags.value = ''; 
    dom.backdrop.classList.remove('hidden');
    dom.settingsModal.classList.remove('hidden');
}

function openGlobalSettings() {
    fetchGlobalSettings();
    dom.backdrop.classList.remove('hidden');
    dom.globalSettingsModal.classList.remove('hidden');
}

window.saveGlobalSettings = async () => {
    const registries = Array.from(document.querySelectorAll('.registry-entry')).map(entry => ({
        url: entry.querySelector('.reg-url').value.trim(),
        username: entry.querySelector('.reg-user').value.trim(),
        password: entry.querySelector('.reg-pass').value.trim()
    })).filter(r => r.url !== '');

    const payload = {
        poll_interval: dom.gs.poll.value,
        retention_policy: dom.gs.retention.value,
        gh_token: dom.gs.token.value,
        registries: registries
    };

    try {
        await fetch(`${API_BASE}/settings`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        closeModals();
        fetchGlobalSettings(); 
        alert("Settings saved. Docker daemon has been authenticated with provided registries.");
    } catch (e) {
        alert("Failed to save global settings: " + e.message);
    }
};

// --- Cross-Registry Push ---
window.addRegistryField = (url = '', username = '', password = '') => {
    const div = document.createElement('div');
    div.className = 'flex gap-2 registry-entry';
    div.innerHTML = `
        <input type="text" placeholder="URL (e.g. docker.io)" value="${url}" class="reg-url w-1/3 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-white text-xs">
        <input type="text" placeholder="Username" value="${username}" class="reg-user w-1/3 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-white text-xs">
        <input type="password" placeholder="Token/Password" value="${password}" class="reg-pass w-1/3 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-white text-xs">
        <button onclick="this.parentElement.remove()" class="text-red-400 hover:text-red-300 px-1">&times;</button>
    `;
    document.getElementById('registry-auth-list').appendChild(div);
};

window.pushBuild = async () => {
    const targetRegistry = dom.pushTargetRegistry.value;
    if (!targetRegistry) return alert("Select a target registry first.");

    const btn = document.querySelector('button[onclick="pushBuild()"]');
    btn.textContent = "Pushing...";
    btn.disabled = true;

    try {
        const res = await fetch(`${API_BASE}/builds/${currentBuildId}/push`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ registry: targetRegistry })
        });
        if (!res.ok) throw new Error(await res.text());
        
        alert(`Successfully pushed build to ${targetRegistry}!`);
    } catch (e) {
        alert("Push failed: " + e.message);
    } finally {
        btn.textContent = "Push";
        btn.disabled = false;
    }
};

window.saveProjectSettings = async () => {
    const payload = {
        repo_url: dom.editRepo.value,
        branch: dom.editBranch.value,
        custom_tags: dom.editTags.value
    };

    try {
        await fetch(`${API_BASE}/projects/${currentProjectId}`, { 
            method: 'PUT', 
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload) 
        });
        closeModals();
        fetchProjects();
    } catch(e) {
        alert("Failed to save settings: " + e.message);
    }
};

window.deleteProject = async () => {
    if(!confirm("Are you sure? This will permanently delete the project, history, and all logs.")) return;
    try {
        await fetch(`${API_BASE}/projects/${currentProjectId}`, { method: 'DELETE' });
        closeModals();
        fetchProjects();
    } catch(e) {
        alert("Failed to delete project: " + e.message);
    }
};

// BUilds and logs
async function openBuildsModal(projectId) {
    currentProjectId = projectId;
    dom.backdrop.classList.remove('hidden');
    dom.buildsModal.classList.remove('hidden');
    
    dom.buildList.innerHTML = `<div class="text-gray-400 text-sm">Loading builds...</div>`;
    dom.logViewer.textContent = "Select a build to view logs...";
    dom.btnDeleteBuild.classList.add('hidden');

    try {
        const res = await fetch(`${API_BASE}/projects/${projectId}/builds`);
        const builds = await res.json();

        // Segregate Active vs Archived
        const active = builds.filter(b => b.status !== 'archived');
        const archived = builds.filter(b => b.status === 'archived');

        const renderItem = (b) => `
            <div onclick="fetchLogs(${b.id}, '${b.status}')" class="p-3 mb-2 ${b.status === 'archived' ? 'bg-gray-800/50' : 'bg-gray-900'} rounded border border-gray-700 cursor-pointer hover:border-gray-500 transition">
                <div class="flex justify-between items-center mb-1">
                    <span class="font-mono text-sm ${b.status === 'archived' ? 'text-gray-500' : 'text-blue-400'}">${b.version}</span>
                    <span class="text-xs ${b.status === 'success' ? 'text-green-400' : b.status === 'failed' ? 'text-red-400' : b.status === 'archived' ? 'text-gray-500' : 'text-blue-400 animate-pulse'}">${b.status}</span>
                </div>
                <div class="text-xs text-gray-500 font-mono">Commit: ${b.commit_hash.substring(0,7)}</div>
            </div>`;

        let html = active.map(renderItem).join('');

        // Wrap archived in a native HTML details tag
        if (archived.length > 0) {
            html += `
            <details class="mt-4 group">
                <summary class="text-xs text-gray-500 cursor-pointer hover:text-gray-300 py-2 select-none flex items-center gap-1 border-t border-gray-700 pt-3">
                    <span class="transform group-open:rotate-90 transition-transform text-[10px]">></span>
                    Archived Builds (${archived.length})
                </summary>
                <div class="mt-2 space-y-2 pl-2 border-l border-gray-700">
                    ${archived.map(renderItem).join('')}
                </div>
            </details>`;
        }

        dom.buildList.innerHTML = html;
    } catch(e) {
        dom.buildList.innerHTML = `<div class="text-red-400 text-sm">Failed to load builds</div>`;
    }
}

async function fetchBuildTags(buildId) {
    dom.existingBuildTags.innerHTML = '<span class="text-xs text-gray-500">Loading tags...</span>';
    try {
        const res = await fetch(`${API_BASE}/builds/${buildId}/tags`);
        const tags = await res.json();
        
        if (tags.length === 0) {
            dom.existingBuildTags.innerHTML = '';
            return;
        }

        dom.existingBuildTags.innerHTML = tags.map(t => `
            <span class="inline-flex items-center gap-1 bg-gray-800 border border-gray-600 text-gray-300 px-2 py-0.5 rounded text-xs">
                ${t}
                <button onclick="deleteBuildTag('${t}')" class="text-red-400 hover:text-red-300 ml-1 font-bold">&times;</button>
            </span>
        `).join('');
    } catch (e) {
        dom.existingBuildTags.innerHTML = '';
    }
}

window.deleteBuildTag = async (tag) => {
    if(!confirm(`Remove tag '${tag}' from the registry?`)) return;
    try {
        await fetch(`${API_BASE}/builds/${currentBuildId}/tags/${tag}`, { method: 'DELETE' });
        fetchBuildTags(currentBuildId);
    } catch (e) {
        alert("Failed to delete tag: " + e.message);
    }
};

window.fetchLogs = async (buildId, status) => {
    currentBuildId = buildId;
    dom.logViewer.textContent = "Connecting to log stream...";
    dom.buildControls.classList.remove('hidden');
    console.log(status);
    if (status==='success'){ 
        dom.btnDeleteBuild.classList.remove("hidden");
    }else{
        dom.btnDeleteBuild.classList.add("hidden");
    }
    dom.newBuildTag.value = ''; 
    fetchBuildTags(buildId);

    clearInterval(logStreamInterval);

    const loadLogText = async () => {
        try {
            const res = await fetch(`${API_BASE}/builds/${buildId}/logs`);
            if (!res.ok) throw new Error("Logs not found");
            const text = await res.text();
            
            const viewer = dom.logViewer;
            const isScrolledToBottom = viewer.scrollHeight - viewer.clientHeight <= viewer.scrollTop + 50;
            
            // Updated fallback string
            viewer.textContent = text || "Log file created. Waiting for Docker daemon output...";
            
            if (isScrolledToBottom || status === 'building') {
                viewer.scrollTop = viewer.scrollHeight;
            }
        } catch (e) {
            dom.logViewer.textContent = "Logs not available yet.";
        }
    };

    await loadLogText();

    if (status === 'building') {
        logStreamInterval = setInterval(async () => {
            await loadLogText();
            
            // Re-fetch project to check if status changed from 'building'
            const projRes = await fetch(`${API_BASE}/projects/${currentProjectId}/builds`);
            const builds = await projRes.json();
            const currentBuild = builds.find(b => b.id === buildId);
            
            if (currentBuild && currentBuild.status !== 'building') {
                clearInterval(logStreamInterval);
                openBuildsModal(currentProjectId);
            }
        }, 1500);
    }
};

window.addBuildTag = async () => {
    const tag = dom.newBuildTag.value.trim();
    if (!tag) return;

    const btn = document.querySelector('button[onclick="addBuildTag()"]');
    btn.textContent = "Tagging...";
    btn.disabled = true;

    try {
        const res = await fetch(`${API_BASE}/builds/${currentBuildId}/tags`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ tag })
        });
        if (!res.ok) throw new Error(await res.text());
        
        alert(`Successfully pushed tag '${tag}' to the registry!`);
        dom.newBuildTag.value = '';
        fetchBuildTags(currentBuildId); 
    } catch (e) {
        alert("Failed to tag image: " + e.message);
    } finally {
        btn.textContent = "Tag Image";
        btn.disabled = false;
    }
};

window.bumpVersion = async (type) => {
    try {
        await fetch(`${API_BASE}/projects/${currentProjectId}/bump`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type })
        });
        const status = document.getElementById('bump-status');
        status.textContent = `${type.charAt(0).toUpperCase() + type.slice(1)} bump scheduled for next build!`;
        status.classList.remove('hidden');
        setTimeout(() => status.classList.add('hidden'), 3000);
    } catch (e) {
        alert("Failed to schedule bump: " + e.message);
    }
};

async function deleteBuild(buildId) {
    if(!confirm("Delete this build and its logs permanently?")) return;
    try {
        await fetch(`${API_BASE}/builds/${buildId}`, { method: 'DELETE' });
        
        dom.logViewer.textContent = "Build deleted.";
        dom.btnDeleteBuild.classList.add('hidden');
        
        // Refresh the builds list in the modal
        if (currentProjectId) openBuildsModal(currentProjectId);
    } catch(e) {
        alert("Failed to delete build: " + e.message);
    }
}


// --- API Interactions ---

async function fetchProjects() {
    try {
        const res = await fetch(`${API_BASE}/projects`);
        if (!res.ok) throw new Error(`API Error: ${res.status}`);
        globalProjects = await res.json(); // Store globally
        renderProjects(globalProjects);
    } catch (err) {
        console.error('Failed to fetch projects:', err);
    }
}

async function handleAddProject(e) {
    e.preventDefault();
    
    const submitBtn = dom.form.querySelector('button[type="submit"]');
    const originalText = submitBtn.innerText;
    submitBtn.innerText = 'Saving...';
    submitBtn.disabled = true;

    const payload = {
        name: dom.inputs.name.value,
        repo_url: dom.inputs.repo.value,
        branch: dom.inputs.branch.value,
        registry_override: dom.inputs.registry.value
    };

    try {
        const res = await fetch(`${API_BASE}/projects`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!res.ok) throw new Error(await res.text() || 'Failed to create project');

        closeModal();
        fetchProjects();
    } catch (err) {
        alert(`Error: ${err.message}`);
    } finally {
        submitBtn.innerText = originalText;
        submitBtn.disabled = false;
    }
}

async function triggerBuild(id) {
    try {
        const res = await fetch(`${API_BASE}/projects/${id}/build`, { method: 'POST' });
        if (!res.ok) throw new Error('Failed to trigger build');
        fetchProjects(); 
    } catch (err) {
        alert(`Error triggering build: ${err.message}`);
    }
}

// --- Rendering ---

function renderProjects(projects) {
    if (!projects || projects.length === 0) {
        dom.projectsContainer.innerHTML = `
            <div class="text-gray-400 text-center py-12 bg-gray-800/50 rounded-lg border border-gray-700 border-dashed animate-pulse">
                <p class="text-lg font-medium text-gray-300">No projects yet</p>
                <p class="text-sm text-gray-500">Add a project to get started</p>
            </div>`;
        return;
    }

    dom.projectsContainer.innerHTML = projects.map(p => {
        const statusColors = {
            'success': 'text-green-400 bg-green-400/10 border-green-400/20',
            'failed': 'text-red-400 bg-red-400/10 border-red-400/20',
            'building': 'text-blue-400 bg-blue-400/10 border-blue-400/20 animate-pulse',
            'pending': 'text-gray-400 bg-gray-400/10 border-gray-400/20'
        };
        const statusClass = statusColors[p.status] || statusColors['pending'];
        const isBuilding = p.status === 'building';

        return `
        <div class="bg-gray-800 border border-gray-700 rounded-lg p-5 shadow-sm hover:border-gray-600 transition-all group">
            <div class="flex justify-between items-start mb-4">
                <div class="flex items-center space-x-3">
                    <div class="p-2 bg-gray-700 rounded-md">
                        <svg class="w-6 h-6 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 002-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"></path></svg>
                    </div>
                    <div>
                        <h3 class="text-lg font-bold text-white tracking-tight">${p.name}</h3>
                        <div class="text-xs text-gray-400 font-mono mt-0.5">${p.repo_url} <span class="text-gray-600 mx-1">|</span> <span class="text-blue-400">${p.branch}</span></div>
                    </div>
                </div>
                <span class="px-2.5 py-0.5 rounded-full text-xs font-medium border flex items-center gap-1.5 ${statusClass} capitalize">
                    ${isBuilding ? '<svg class="animate-spin w-3 h-3" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>' : '<span class="w-1.5 h-1.5 rounded-full bg-current"></span>'}
                    ${p.status || 'pending'}
                </span>
            </div>
            
            <div class="flex justify-between items-end border-t border-gray-700/50 pt-4 mt-2">
                <div class="flex flex-col">
                    <span class="text-[10px] uppercase tracking-wider font-semibold text-gray-500 mb-0.5">Current Version</span>
                    <span class="text-sm font-mono text-gray-200">${p.version || '0.0.0'}</span>
                </div>
                
                <div class="flex items-center gap-2">
                    <button 
                        data-action="logs" 
                        data-id="${p.id}"
                        class="bg-gray-700 hover:bg-gray-600 text-gray-200 px-3 py-2 rounded-md text-sm font-medium transition-colors border border-gray-600 shadow-sm flex items-center"
                        title="View Build History & Logs"
                    >
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7"></path></svg>
                    </button>
                    <button 
                        data-action="settings" 
                        data-id="${p.id}"
                        class="bg-gray-700 hover:bg-gray-600 text-gray-200 px-3 py-2 rounded-md text-sm font-medium transition-colors border border-gray-600 shadow-sm flex items-center"
                        title="Project Settings"
                    >
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"></path></svg>
                    </button>
                    <button 
                        data-action="build" 
                        data-id="${p.id}"
                        class="bg-gray-700 hover:bg-blue-600 hover:text-white text-gray-200 px-4 py-2 rounded-md text-sm font-medium transition-colors border border-gray-600 shadow-sm flex items-center gap-2 group-hover:border-gray-500 ${isBuilding ? 'opacity-50 cursor-not-allowed' : ''}"
                        ${isBuilding ? 'disabled' : ''}
                    >
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                        Build Now
                    </button>
                </div>
            </div>
        </div>
        `;
    }).join('');
}

init();